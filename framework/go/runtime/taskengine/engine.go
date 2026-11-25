package taskengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

type EngineOptions struct {
	Workers           int
	PollInterval      time.Duration
	RetryScanInterval time.Duration
	LeaseDuration     time.Duration
	RecoveryBatch     int
	WorkerPrefix      string
	StopErrorCode     string
	ResourcePool      *resourcepool.Pool
	OwnResourcePool   bool
	Clock             clock.Clock
}

type Engine struct {
	repository Repository
	registry   *Registry
	options    EngineOptions

	lifecycleMu sync.Mutex
	started     bool
	cancel      context.CancelFunc
	done        chan struct{}
	wait        sync.WaitGroup
	leaseGate   chan struct{}

	runningMu sync.Mutex
	running   map[string]context.CancelFunc
}

func NewEngine(repository Repository, registry *Registry, options EngineOptions) (*Engine, error) {
	if options.StopErrorCode == "" {
		options.StopErrorCode = "WORKER_STOPPED"
	}
	if repository == nil || registry == nil || options.Workers < 1 || options.Workers > 64 ||
		options.PollInterval < time.Millisecond || options.RetryScanInterval < time.Millisecond ||
		options.LeaseDuration < 100*time.Millisecond || options.LeaseDuration > time.Hour ||
		options.RecoveryBatch < 1 || options.RecoveryBatch > 1000 || options.WorkerPrefix == "" ||
		!identifierPattern.MatchString(options.StopErrorCode) || (options.OwnResourcePool && options.ResourcePool == nil) {
		return nil, fmt.Errorf("%w: task engine 选项无效", ErrInvalidArgument)
	}
	if options.Clock == nil {
		options.Clock = clock.System{}
	}
	leaseGate := make(chan struct{}, 1)
	leaseGate <- struct{}{}
	return &Engine{
		repository: repository,
		registry:   registry,
		options:    options,
		done:       make(chan struct{}),
		leaseGate:  leaseGate,
		running:    make(map[string]context.CancelFunc),
	}, nil
}

func (engine *Engine) now() time.Time {
	if engine.options.Clock == nil {
		return time.Now().UTC()
	}
	return engine.options.Clock.Now()
}

func (engine *Engine) Start(parent context.Context) error {
	engine.lifecycleMu.Lock()
	defer engine.lifecycleMu.Unlock()
	if engine.started {
		return fmt.Errorf("%w: task engine 已启动", ErrConflict)
	}
	if parent == nil {
		return fmt.Errorf("%w: root context 不能为空", ErrInvalidArgument)
	}
	ctx, cancel := context.WithCancel(parent)
	if _, err := engine.repository.RecoverExpired(ctx, engine.now(), engine.options.RecoveryBatch, engine.registry.Resumable); err != nil {
		cancel()
		return fmt.Errorf("恢复持久任务: %w", err)
	}
	if engine.options.ResourcePool != nil {
		var err error
		if engine.options.OwnResourcePool {
			err = engine.options.ResourcePool.Start(ctx)
		} else {
			err = engine.options.ResourcePool.Ready(ctx)
		}
		if err != nil {
			cancel()
			return fmt.Errorf("准备任务资源池: %w", err)
		}
	}
	engine.started = true
	engine.cancel = cancel

	for index := 0; index < engine.options.Workers; index++ {
		engine.wait.Add(1)
		go engine.worker(ctx, fmt.Sprintf("%s-%d", engine.options.WorkerPrefix, index+1))
	}
	engine.wait.Add(1)
	go engine.maintenance(ctx)
	go func() {
		engine.wait.Wait()
		close(engine.done)
	}()
	return nil
}

func (engine *Engine) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: close context 不能为空", ErrInvalidArgument)
	}
	engine.lifecycleMu.Lock()
	if !engine.started {
		engine.lifecycleMu.Unlock()
		return nil
	}
	engine.cancel()
	done := engine.done
	engine.lifecycleMu.Unlock()
	var result error
	select {
	case <-done:
		result = nil
	case <-ctx.Done():
		result = ctx.Err()
	}
	if engine.options.OwnResourcePool && engine.options.ResourcePool != nil {
		result = errors.Join(result, engine.options.ResourcePool.Close(ctx))
	}
	return result
}

func (engine *Engine) Cancel(ctx context.Context, taskID, _ string) (Task, error) {
	task, err := engine.repository.RequestCancel(ctx, taskID, engine.now())
	if err != nil {
		return Task{}, err
	}
	engine.cancelRunning(task)
	return task, nil
}

func (engine *Engine) CancelIdempotent(ctx context.Context, taskID, reason, idempotencyKey string) (Task, error) {
	task, _, err := engine.repository.RequestCancelIdempotent(ctx, taskID, reason, idempotencyKey, engine.now())
	if err != nil {
		return Task{}, err
	}
	engine.cancelRunning(task)
	return task, nil
}

func (engine *Engine) cancelRunning(task Task) {
	if task.State == StateRunning {
		engine.runningMu.Lock()
		cancel := engine.running[task.ID]
		engine.runningMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (engine *Engine) worker(ctx context.Context, owner string) {
	defer engine.wait.Done()
	for {
		task, found := engine.waitForTask(ctx, owner)
		if !found {
			return
		}
		engine.execute(ctx, owner, task)
	}
}

// waitForTask 合并空队列与查询失败时的等待；领取成功即释放许可，不限制任务执行并发。
func (engine *Engine) waitForTask(ctx context.Context, owner string) (Task, bool) {
	select {
	case <-ctx.Done():
		return Task{}, false
	case <-engine.leaseGate:
	}
	defer func() { engine.leaseGate <- struct{}{} }()
	for ctx.Err() == nil {
		now := engine.now()
		task, found, err := engine.repository.LeaseNext(ctx, LeaseRequest{
			Owner: owner, Now: now, Duration: engine.options.LeaseDuration,
		})
		if err == nil && found {
			return task, true
		}
		// 只有许可持有者运行计时器，其他 worker 可取消地等待，避免空闲唤醒风暴。
		if !waitContext(ctx, engine.options.PollInterval) {
			return Task{}, false
		}
	}
	return Task{}, false
}

func (engine *Engine) execute(engineCtx context.Context, owner string, leased Task) {
	now := engine.now()
	running, _, err := engine.repository.Start(engineCtx, leased.ID, owner, now)
	if err != nil {
		return
	}
	registration, found := engine.registry.Lookup(running.Type)
	if !found {
		engine.finish(running, Command{
			Kind: CommandFail, Now: now, LeaseOwner: owner,
			ErrorCode: "HANDLER_NOT_FOUND", ErrorSummary: "任务处理器未注册",
		})
		return
	}

	if spanCtx, _, spanErr := startTaskSpan(engineCtx, running); spanErr == nil {
		engineCtx = spanCtx
	}
	executionCtx, cancelExecution := context.WithTimeout(engineCtx, registration.Timeout)
	engine.track(running.ID, cancelExecution)
	if latest, getErr := engine.repository.Get(engineCtx, running.ID); getErr == nil && latest.CancelRequestedAt != nil {
		cancelExecution()
	}
	var leaseLost atomic.Bool
	renewDone := make(chan struct{})
	renewCtx, stopRenew := context.WithCancel(engineCtx)
	go func() {
		defer close(renewDone)
		engine.renewLease(renewCtx, running.ID, owner, cancelExecution, &leaseLost)
	}()

	result := engine.dispatch(executionCtx, registration, running, repositoryOutput{
		repository: engine.repository, taskID: running.ID, attemptNo: running.AttemptCount,
	})
	stopRenew()
	<-renewDone
	engine.untrack(running.ID)
	cancelExecution()

	if leaseLost.Load() {
		return
	}
	finishedAt := engine.now()
	latest, getErr := engine.repository.Get(context.Background(), running.ID)
	if getErr == nil && latest.CancelRequestedAt != nil {
		engine.finish(running, Command{Kind: CommandSucceed, Now: finishedAt, LeaseOwner: owner})
		return
	}
	if engineCtx.Err() != nil {
		engine.finish(running, Command{
			Kind: CommandInterrupt, Now: finishedAt, LeaseOwner: owner, ErrorCode: engine.options.StopErrorCode,
		})
		return
	}
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		result = ExecutionResult{
			ErrorCode: "DEADLINE_EXCEEDED", ErrorSummary: "任务处理超过期限", Retryable: true, RetryAfter: time.Second,
		}
	}
	if result.ErrorCode == "" && len(result.Summary) > 0 && !json.Valid(result.Summary) {
		result = ExecutionResult{ErrorCode: "INVALID_RESULT", ErrorSummary: "任务处理器返回了无效结果"}
	}
	if result.ErrorCode == "" {
		engine.finish(running, Command{
			Kind: CommandSucceed, Now: finishedAt, LeaseOwner: owner, ResultSummary: string(result.Summary),
		})
		return
	}
	retryAfter := result.RetryAfter
	if result.Retryable && retryAfter <= 0 {
		retryAfter = time.Second
	}
	engine.finish(running, Command{
		Kind: CommandFail, Now: finishedAt, LeaseOwner: owner,
		Retryable: result.Retryable, RetryAt: finishedAt.Add(retryAfter),
		ErrorCode: result.ErrorCode, ErrorSummary: result.ErrorSummary,
	})
}

func (engine *Engine) dispatch(ctx context.Context, registration Registration, task Task, output OutputWriter) ExecutionResult {
	if engine.options.ResourcePool == nil {
		return executeSafely(ctx, registration.Handler, task, output)
	}
	var executionResult ExecutionResult
	future, err := engine.options.ResourcePool.Submit(ctx, resourcepool.Job{
		ID: task.ID + ":" + fmt.Sprint(task.AttemptCount), Group: registration.ResourceGroup, Priority: task.Priority,
		Execute: func(executionContext context.Context) error {
			executionResult = executeSafely(executionContext, registration.Handler, task, output)
			return nil
		},
	})
	if err != nil {
		return resourceExecutionFailure(err)
	}
	// 任务 Context 负责通知 Handler 取消；持久终态必须等待 Handler 实际退出后再提交。
	result, waitErr := future.Wait(context.Background())
	if waitErr != nil {
		return resourceExecutionFailure(waitErr)
	}
	if result.Err != nil {
		return resourceExecutionFailure(result.Err)
	}
	return executionResult
}

func resourceExecutionFailure(err error) ExecutionResult {
	switch {
	case errors.Is(err, resourcepool.ErrQueueFull):
		return ExecutionResult{ErrorCode: "RESOURCE_EXHAUSTED", ErrorSummary: "任务资源组队列已满", Retryable: true, RetryAfter: time.Second}
	case errors.Is(err, resourcepool.ErrCanceled), errors.Is(err, context.Canceled):
		return ExecutionResult{ErrorCode: "CANCELLED", ErrorSummary: "任务资源执行已取消"}
	case errors.Is(err, context.DeadlineExceeded):
		return ExecutionResult{ErrorCode: "DEADLINE_EXCEEDED", ErrorSummary: "任务资源执行超过期限", Retryable: true, RetryAfter: time.Second}
	case errors.Is(err, resourcepool.ErrClosed), errors.Is(err, resourcepool.ErrNotStarted):
		return ExecutionResult{ErrorCode: "EXECUTOR_UNAVAILABLE", ErrorSummary: "任务资源池不可用", Retryable: true, RetryAfter: time.Second}
	default:
		return ExecutionResult{ErrorCode: "EXECUTOR_FAILED", ErrorSummary: "任务资源执行失败"}
	}
}

func (engine *Engine) finish(task Task, command Command) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = engine.repository.Finish(ctx, task.ID, command)
}

func (engine *Engine) renewLease(ctx context.Context, taskID, owner string, cancel context.CancelFunc, lost *atomic.Bool) {
	interval := engine.options.LeaseDuration / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := engine.repository.RenewLease(ctx, taskID, owner, now.UTC(), engine.options.LeaseDuration); err != nil {
				if ctx.Err() == nil {
					lost.Store(true)
					cancel()
				}
				return
			}
		}
	}
}

func (engine *Engine) maintenance(ctx context.Context) {
	defer engine.wait.Done()
	ticker := time.NewTicker(engine.options.RetryScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			// 新进程启动时旧租约可能尚未过期，必须持续回收，不能只检查启动瞬间。
			_, _ = engine.repository.RecoverExpired(ctx, now.UTC(), engine.options.RecoveryBatch, engine.registry.Resumable)
			_, _ = engine.repository.PromoteDueRetries(ctx, now.UTC(), engine.options.RecoveryBatch)
		}
	}
}

func (engine *Engine) track(taskID string, cancel context.CancelFunc) {
	engine.runningMu.Lock()
	defer engine.runningMu.Unlock()
	engine.running[taskID] = cancel
}

func (engine *Engine) untrack(taskID string) {
	engine.runningMu.Lock()
	defer engine.runningMu.Unlock()
	delete(engine.running, taskID)
}

type repositoryOutput struct {
	repository Repository
	taskID     string
	attemptNo  int
}

func (output repositoryOutput) SaveCheckpoint(ctx context.Context, key string, payload json.RawMessage) (Checkpoint, bool, error) {
	return output.repository.SaveCheckpoint(ctx, CheckpointRequest{
		TaskID: output.taskID, AttemptNo: output.attemptNo, Key: key, Payload: payload, Now: time.Now().UTC(),
	})
}

func (output repositoryOutput) LatestCheckpoint(ctx context.Context) (Checkpoint, bool, error) {
	return output.repository.LatestCheckpoint(ctx, output.taskID)
}

func (output repositoryOutput) Write(ctx context.Context, kind string, payload []byte) error {
	_, err := output.repository.AppendOutput(ctx, output.taskID, kind, payload, time.Now().UTC())
	return err
}

func executeSafely(ctx context.Context, handler Handler, task Task, output OutputWriter) (result ExecutionResult) {
	defer func() {
		if recover() != nil {
			result = ExecutionResult{ErrorCode: "INTERNAL", ErrorSummary: "任务处理器发生异常"}
		}
	}()
	return handler.Execute(ctx, task, output)
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
