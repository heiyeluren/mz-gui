package taskengine

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

type startWindowRepository struct {
	Repository
	started chan struct{}
	release chan struct{}
}

func (repository *startWindowRepository) Start(ctx context.Context, taskID, owner string, now time.Time) (Task, Attempt, error) {
	task, attempt, err := repository.Repository.Start(ctx, taskID, owner, now)
	if err == nil {
		close(repository.started)
		<-repository.release
	}
	return task, attempt, err
}

func TestEngineExecutesPersistedTaskAndWritesOutput(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	executed := make(chan struct{}, 1)
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true,
		Timeout:   time.Second,
		Handler: HandlerFunc(func(ctx context.Context, task Task, output OutputWriter) ExecutionResult {
			if err := output.Write(ctx, "stdout", []byte("hello")); err != nil {
				return ExecutionResult{ErrorCode: "OUTPUT_FAILED", ErrorSummary: err.Error()}
			}
			executed <- struct{}{}
			return ExecutionResult{Summary: json.RawMessage(`{"ok":true}`)}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "engine-success", 1, time.Now().UTC(), nil)
	startEngine(t, engine)

	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未执行")
	}
	finished := waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	if finished.AttemptCount != 1 || finished.ResultSummaryJSON != `{"ok":true}` {
		t.Fatalf("任务完成快照错误: %#v", finished)
	}
	outputs, err := repository.ReadOutput(context.Background(), task.ID, 0, 10)
	if err != nil || len(outputs) != 1 || string(outputs[0].Payload) != "hello" {
		t.Fatalf("任务输出错误: outputs=%#v err=%v", outputs, err)
	}
}

func TestEngineEnforcesWorkerConcurrencyLimit(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	release := make(chan struct{})
	started := make(chan struct{}, 5)
	var active atomic.Int32
	var maximum atomic.Int32
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: 2 * time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			current := active.Add(1)
			for {
				old := maximum.Load()
				if current <= old || maximum.CompareAndSwap(old, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			return ExecutionResult{}
		}),
	})
	engine := newTestEngine(t, repository, registry, 2)
	now := time.Now().UTC()
	for index := 0; index < 5; index++ {
		createTask(t, repository, "bounded-"+string(rune('a'+index)), 1, now.Add(time.Duration(index)*time.Millisecond), nil)
	}
	startEngine(t, engine)
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("worker 未按并发上限启动")
		}
	}
	select {
	case <-started:
		t.Fatal("第三个任务在 worker 槽位释放前启动")
	case <-time.After(100 * time.Millisecond):
	}
	if maximum.Load() != 2 {
		t.Fatalf("最大并发 = %d", maximum.Load())
	}
	close(release)
}

func TestEngineDispatchesHandlersThroughIsolatedResourceGroups(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	release := make(chan struct{})
	started := make(chan string, 4)
	register := func(taskType string, group resourcepool.Group) {
		mustRegister(t, registry, taskType, Registration{
			ResourceGroup: group, Resumable: true, Timeout: 2 * time.Second,
			Handler: HandlerFunc(func(ctx context.Context, task Task, _ OutputWriter) ExecutionResult {
				started <- task.ID
				select {
				case <-release:
					return ExecutionResult{}
				case <-ctx.Done():
					return ExecutionResult{ErrorCode: "CANCELLED"}
				}
			}),
		})
	}
	register("test.cpu", resourcepool.GroupCPU)
	register("test.network", resourcepool.GroupNetwork)
	pool, err := resourcepool.New([]resourcepool.GroupConfig{
		{Name: resourcepool.GroupCPU, Workers: 1, QueueCapacity: 4},
		{Name: resourcepool.GroupNetwork, Workers: 2, QueueCapacity: 4},
	}, resourcepool.Options{Name: "task-resource-pool"})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 4, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "resource-worker",
		ResourcePool: pool, OwnResourcePool: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, engine) })
	now := time.Now().UTC()
	tasks := []Task{
		createTypedTask(t, repository, "test.cpu", "cpu-1", resourcepool.PriorityNormal, now),
		createTypedTask(t, repository, "test.cpu", "cpu-2", resourcepool.PriorityNormal, now.Add(time.Millisecond)),
		createTypedTask(t, repository, "test.network", "network-1", resourcepool.PriorityNormal, now.Add(2*time.Millisecond)),
		createTypedTask(t, repository, "test.network", "network-2", resourcepool.PriorityNormal, now.Add(3*time.Millisecond)),
	}
	startEngine(t, engine)
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("持久任务未按资源组配额启动: seen=%v snapshot=%#v", seen, pool.Snapshot())
		}
	}
	cpuStarted := 0
	for _, task := range tasks[:2] {
		if seen[task.ID] {
			cpuStarted++
		}
	}
	if cpuStarted != 1 {
		t.Fatalf("CPU 资源组并发 = %d，期望 1: seen=%v", cpuStarted, seen)
	}
	close(release)
	for _, task := range tasks {
		waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !pool.Snapshot().Closed {
		t.Fatal("taskengine 必须关闭其拥有的资源池")
	}
}

func TestEngineUsesSharedResourcePoolWithoutTakingOwnership(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult { return ExecutionResult{} }),
	})
	pool, err := resourcepool.New([]resourcepool.GroupConfig{
		{Name: resourcepool.GroupDefault, Workers: 1, QueueCapacity: 2},
	}, resourcepool.Options{Name: "shared-task-pool"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "shared-worker", ResourcePool: pool,
	})
	if err != nil {
		t.Fatal(err)
	}
	startEngine(t, engine)
	closeEngine(t, engine)
	if err := pool.Ready(context.Background()); err != nil {
		t.Fatalf("taskengine 不得关闭共享资源池: %v", err)
	}

	unstarted, err := resourcepool.New([]resourcepool.GroupConfig{
		{Name: resourcepool.GroupDefault, Workers: 1, QueueCapacity: 1},
	}, resourcepool.Options{Name: "unstarted-task-pool"})
	if err != nil {
		t.Fatal(err)
	}
	invalidEngine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "unready-worker", ResourcePool: unstarted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := invalidEngine.Start(context.Background()); !errors.Is(err, resourcepool.ErrNotStarted) {
		t.Fatalf("未就绪共享资源池错误 = %v", err)
	}
}

func TestEngineWaitsForCanceledResourceHandlerCleanup(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	started := make(chan struct{})
	cancelObserved := make(chan struct{})
	releaseCleanup := make(chan struct{})
	mustRegister(t, registry, "test.cleanup", Registration{
		ResourceGroup: resourcepool.GroupCPU, Resumable: false, Timeout: time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			close(started)
			<-ctx.Done()
			close(cancelObserved)
			<-releaseCleanup
			return ExecutionResult{ErrorCode: "CANCELLED"}
		}),
	})
	pool, err := resourcepool.New([]resourcepool.GroupConfig{
		{Name: resourcepool.GroupCPU, Workers: 1, QueueCapacity: 1},
	}, resourcepool.Options{Name: "cleanup-task-pool"})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "cleanup-worker",
		ResourcePool: pool, OwnResourcePool: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, engine) })
	task := createTypedTask(t, repository, "test.cleanup", "cleanup", resourcepool.PriorityInteractive, time.Now().UTC())
	startEngine(t, engine)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("资源 Handler 未启动")
	}
	if _, err := engine.Cancel(context.Background(), task.ID, "test"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelObserved:
	case <-time.After(time.Second):
		t.Fatal("取消未传播到资源 Handler")
	}
	current, err := repository.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State.Terminal() {
		t.Fatalf("Handler 清理完成前不得提交任务终态: %#v", current)
	}
	close(releaseCleanup)
	waitTaskState(t, repository, task.ID, StateCancelled, time.Second)
}

func TestResourceExecutionFailureUsesStableCodes(t *testing.T) {
	tests := []struct {
		err       error
		code      string
		retryable bool
	}{
		{err: resourcepool.ErrQueueFull, code: "RESOURCE_EXHAUSTED", retryable: true},
		{err: resourcepool.ErrCanceled, code: "CANCELLED"},
		{err: context.Canceled, code: "CANCELLED"},
		{err: context.DeadlineExceeded, code: "DEADLINE_EXCEEDED", retryable: true},
		{err: resourcepool.ErrClosed, code: "EXECUTOR_UNAVAILABLE", retryable: true},
		{err: resourcepool.ErrNotStarted, code: "EXECUTOR_UNAVAILABLE", retryable: true},
		{err: resourcepool.ErrJobPanic, code: "EXECUTOR_FAILED"},
	}
	for _, test := range tests {
		result := resourceExecutionFailure(test.err)
		if result.ErrorCode != test.code || result.Retryable != test.retryable {
			t.Fatalf("错误 %v 映射为 %#v", test.err, result)
		}
	}
}

func TestEngineCancelPropagatesToRunningHandler(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	started := make(chan struct{})
	observedCancel := make(chan struct{})
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: 5 * time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			close(started)
			<-ctx.Done()
			close(observedCancel)
			return ExecutionResult{ErrorCode: "CANCELLED", ErrorSummary: "cancelled"}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "cancel-running", 1, time.Now().UTC(), nil)
	startEngine(t, engine)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler 未启动")
	}
	if _, err := engine.Cancel(context.Background(), task.ID, "user requested"); err != nil {
		t.Fatalf("取消任务: %v", err)
	}
	select {
	case <-observedCancel:
	case <-time.After(time.Second):
		t.Fatal("取消未传播到 handler context")
	}
	waitTaskState(t, repository, task.ID, StateCancelled, 2*time.Second)
}

func TestEngineCancelIdempotentDelegatesToRepository(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult { return ExecutionResult{} }),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "cancel-idempotent", 1, time.Now().UTC(), nil)
	cancelled, err := engine.CancelIdempotent(context.Background(), task.ID, "用户取消", "cancel-idempotent-key")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.State != StateCancelled || cancelled.CancelRequestedAt == nil {
		t.Fatalf("幂等取消结果错误: %#v", cancelled)
	}
	replayed, err := engine.CancelIdempotent(context.Background(), task.ID, "用户取消", "cancel-idempotent-key")
	if err != nil || replayed.Revision != cancelled.Revision {
		t.Fatalf("幂等取消重放错误: task=%#v err=%v", replayed, err)
	}
}

func TestEngineCancelDuringStartWindowReachesHandlerContext(t *testing.T) {
	baseRepository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	repository := &startWindowRepository{Repository: baseRepository, started: make(chan struct{}), release: make(chan struct{})}
	registry := NewRegistry()
	observedCancel := make(chan struct{})
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			<-ctx.Done()
			close(observedCancel)
			return ExecutionResult{ErrorCode: "CANCELLED"}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, baseRepository, "cancel-start-window", 1, time.Now().UTC(), nil)
	startEngine(t, engine)
	select {
	case <-repository.started:
	case <-time.After(time.Second):
		t.Fatal("任务未进入启动窗口")
	}
	if _, err := engine.Cancel(context.Background(), task.ID, "user requested"); err != nil {
		t.Fatal(err)
	}
	close(repository.release)
	select {
	case <-observedCancel:
	case <-time.After(time.Second):
		t.Fatal("启动窗口内的取消未传播到 handler")
	}
	waitTaskState(t, baseRepository, task.ID, StateCancelled, time.Second)
}

func TestEngineRejectsInvalidHandlerResultJSON(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
			return ExecutionResult{Summary: json.RawMessage(`not-json`)}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "invalid-handler-result", 1, time.Now().UTC(), nil)
	startEngine(t, engine)
	failed := waitTaskState(t, repository, task.ID, StateFailed, time.Second)
	if failed.ErrorCode != "INVALID_RESULT" {
		t.Fatalf("非法结果错误码 = %q", failed.ErrorCode)
	}
}

func TestEngineIsolatesHandlerPanicAndContinues(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.panic", Registration{
		Resumable: false, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
			panic("sensitive panic detail")
		}),
	})
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
			return ExecutionResult{}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	now := time.Now().UTC()
	panicTask := createTypedTask(t, repository, "test.panic", "panic", 10, now)
	normalTask := createTypedTask(t, repository, "test.echo", "normal", 1, now.Add(time.Millisecond))
	startEngine(t, engine)

	failed := waitTaskState(t, repository, panicTask.ID, StateFailed, 2*time.Second)
	if failed.ErrorCode != "INTERNAL" {
		t.Fatalf("panic 错误码 = %q", failed.ErrorCode)
	}
	waitTaskState(t, repository, normalTask.ID, StateSucceeded, 2*time.Second)
}

func TestEngineAutomaticallyRetriesWithNewAttempt(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	var calls atomic.Int32
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
			if calls.Add(1) == 1 {
				return ExecutionResult{ErrorCode: "UNAVAILABLE", ErrorSummary: "temporary", Retryable: true, RetryAfter: 20 * time.Millisecond}
			}
			return ExecutionResult{}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "retry-engine", 1, time.Now().UTC(), nil)
	startEngine(t, engine)

	finished := waitTaskState(t, repository, task.ID, StateSucceeded, 3*time.Second)
	if finished.AttemptCount != 2 || calls.Load() != 2 {
		t.Fatalf("自动重试错误: task=%#v calls=%d", finished, calls.Load())
	}
}

func TestEngineRenewsLeaseForLongTask(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: 2 * time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ExecutionResult{ErrorCode: "CANCELLED"}
			}
			return ExecutionResult{}
		}),
	})
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: 300 * time.Millisecond, RecoveryBatch: 100, WorkerPrefix: "test-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, engine) })
	task := createTask(t, repository, "renew-engine", 1, time.Now().UTC(), nil)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("长任务未启动")
	}
	time.Sleep(500 * time.Millisecond)
	running, err := repository.Get(context.Background(), task.ID)
	if err != nil || running.State != StateRunning || running.LeaseExpiresAt == nil || !running.LeaseExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("长任务未续租: task=%#v err=%v", running, err)
	}
	close(release)
	waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
}

func TestEngineShutdownInterruptsAndNextStartResumesTask(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: 2 * time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-ctx.Done()
			}
			return ExecutionResult{}
		}),
	})
	task := createTask(t, repository, "restart-resume", 1, time.Now().UTC(), nil)
	first, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "test-worker", StopErrorCode: "CUSTOM_STOPPED",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, first) })
	startEngine(t, first)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("首个 attempt 未启动")
	}
	closeEngine(t, first)
	interrupted := waitTaskState(t, repository, task.ID, StateInterrupted, time.Second)
	if interrupted.AttemptCount != 1 || interrupted.ErrorCode != "CUSTOM_STOPPED" {
		t.Fatalf("中断 attempt 错误: %#v", interrupted)
	}

	second := newTestEngine(t, repository, registry, 1)
	startEngine(t, second)
	finished := waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	if finished.AttemptCount != 2 || calls.Load() != 2 {
		t.Fatalf("重启恢复错误: task=%#v calls=%d", finished, calls.Load())
	}
}

func TestEngineFailsUnknownHandlerWithoutStoppingWorker(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	engine := newTestEngine(t, repository, registry, 1)
	task := createTypedTask(t, repository, "test.unknown", "unknown-handler", 1, time.Now().UTC())
	startEngine(t, engine)
	failed := waitTaskState(t, repository, task.ID, StateFailed, 2*time.Second)
	if failed.ErrorCode != "HANDLER_NOT_FOUND" {
		t.Fatalf("未注册 handler 错误码 = %q", failed.ErrorCode)
	}
}

func TestEngineTurnsHandlerDeadlineIntoStableFailure(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.timeout", Registration{
		Resumable: true, Timeout: 100 * time.Millisecond,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			<-ctx.Done()
			return ExecutionResult{}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task, _, err := repository.Create(context.Background(), CreateRequest{
		Type: "test.timeout", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "timeout", MaxAttempts: 1, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	startEngine(t, engine)
	failed := waitTaskState(t, repository, task.ID, StateFailed, 2*time.Second)
	if failed.ErrorCode != "DEADLINE_EXCEEDED" {
		t.Fatalf("deadline 错误码 = %q", failed.ErrorCode)
	}
}

func TestRegistryAndEngineValidateLifecycle(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("bad type", Registration{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法注册错误 = %v", err)
	}
	registration := Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult { return ExecutionResult{} }),
	}
	if err := registry.Register("test.echo", registration); err != nil {
		t.Fatal(err)
	}
	registered, found := registry.Lookup("test.echo")
	if !found || registered.ResourceGroup != resourcepool.GroupDefault {
		t.Fatalf("未声明资源组必须规范化到 default: %#v", registered)
	}
	invalidGroup := registration
	invalidGroup.ResourceGroup = "bad group"
	if err := registry.Register("test.invalid-group", invalidGroup); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法资源组必须被拒绝: %v", err)
	}
	if err := registry.Register("test.echo", registration); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复注册错误 = %v", err)
	}
	if registry.Resumable(Task{Type: "missing"}) {
		t.Fatal("未注册任务不能声明可恢复")
	}

	if _, err := NewEngine(nil, registry, EngineOptions{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 engine 选项错误 = %v", err)
	}
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	engine := newTestEngine(t, repository, registry, 1)
	var nilContext context.Context
	if err := engine.Start(nilContext); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil context 错误 = %v", err)
	}
	startEngine(t, engine)
	if err := engine.Start(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复启动错误 = %v", err)
	}
}

func newTestEngine(t *testing.T, repository Repository, registry *Registry, workers int) *Engine {
	t.Helper()
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: workers, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "test-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, engine) })
	return engine
}

func startEngine(t *testing.T, engine *Engine) {
	t.Helper()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func closeEngine(t *testing.T, engine *Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := engine.Close(ctx); err != nil {
		t.Fatalf("关闭 task engine: %v", err)
	}
}

func mustRegister(t *testing.T, registry *Registry, taskType string, registration Registration) {
	t.Helper()
	if err := registry.Register(taskType, registration); err != nil {
		t.Fatalf("注册 handler %s: %v", taskType, err)
	}
}

func createTypedTask(t *testing.T, repository *SQLiteRepository, taskType, key string, priority int, now time.Time) Task {
	t.Helper()
	task, created, err := repository.Create(context.Background(), CreateRequest{
		Type: taskType, Priority: priority, PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: key, MaxAttempts: 2, Now: now,
	})
	if err != nil || !created {
		t.Fatalf("创建任务 %s: created=%v err=%v", key, created, err)
	}
	return task
}

func waitTaskState(t *testing.T, repository *SQLiteRepository, taskID string, state State, timeout time.Duration) Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := repository.Get(context.Background(), taskID)
		if err == nil && task.State == state {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	task, err := repository.Get(context.Background(), taskID)
	t.Fatalf("等待任务状态 %s 超时: task=%#v err=%v", state, task, err)
	return Task{}
}
