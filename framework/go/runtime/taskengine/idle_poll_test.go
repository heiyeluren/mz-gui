package taskengine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// idlePollRepository 统计真实领取，并可注入可恢复的查询失败。
type idlePollRepository struct {
	Repository
	calls atomic.Int64
	fail  atomic.Bool
	idle  chan struct{}
}

func (repository *idlePollRepository) LeaseNext(ctx context.Context, request LeaseRequest) (Task, bool, error) {
	repository.calls.Add(1)
	var task Task
	var found bool
	var err error
	if repository.fail.Load() {
		err = errors.New("测试领取失败")
	} else {
		task, found, err = repository.Repository.LeaseNext(ctx, request)
	}
	if err != nil || !found {
		select {
		case repository.idle <- struct{}{}:
		default:
		}
	}
	return task, found, err
}

func TestEngineSharesIdlePollingAndCancelsWaiters(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("query_error_%t", fail), func(t *testing.T) {
			repository, closeDatabase := newTestRepository(t)
			defer closeDatabase()
			counted := &idlePollRepository{Repository: repository, idle: make(chan struct{}, 1)}
			counted.fail.Store(fail)
			engine, err := NewEngine(counted, NewRegistry(), EngineOptions{
				Workers: 62, PollInterval: time.Hour, RetryScanInterval: time.Hour,
				LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "idle-test",
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeEngine(t, engine) })
			startEngine(t, engine)
			select {
			case <-counted.idle:
			case <-time.After(time.Second):
				t.Fatal("没有执行首次领取")
			}
			time.Sleep(50 * time.Millisecond)
			if calls := counted.calls.Load(); calls != 1 {
				t.Errorf("一个空闲周期执行了 %d 次领取，期望共享一次", calls)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := engine.Close(ctx); err != nil {
				t.Fatalf("取消应立即释放轮询者与等待 worker: %v", err)
			}
		})
	}
}

func TestEngineIdleAndErrorRecoveryKeepExecutionConcurrency(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_query_error_%t", fail), func(t *testing.T) {
			repository, closeDatabase := newTestRepository(t)
			defer closeDatabase()
			counted := &idlePollRepository{Repository: repository, idle: make(chan struct{}, 1)}
			counted.fail.Store(fail)
			const workers = 8
			started := make(chan struct{}, workers)
			release := make(chan struct{})
			defer close(release)
			registry := NewRegistry()
			mustRegister(t, registry, "test.echo", Registration{
				Resumable: true, Timeout: 5 * time.Second,
				Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
					started <- struct{}{}
					select {
					case <-release:
					case <-ctx.Done():
					}
					return ExecutionResult{}
				}),
			})
			engine := newTestEngine(t, counted, registry, workers)
			defer closeEngine(t, engine)
			startEngine(t, engine)
			select {
			case <-counted.idle:
			case <-time.After(time.Second):
				t.Fatal("引擎没有进入空闲或错误等待")
			}
			for i := 0; i < workers; i++ {
				createTask(t, repository, fmt.Sprintf("idle-resume-%d", i), 1, time.Now().UTC(), nil)
			}
			counted.fail.Store(false)
			deadline := time.After(2 * time.Second)
			for i := 0; i < workers; i++ {
				select {
				case <-started:
				case <-deadline:
					t.Fatalf("仅 %d/%d 个任务并发启动，领取等待不能串行化执行", i, workers)
				}
			}
		})
	}
}
