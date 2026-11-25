package resourcepool

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestPoolIsolatesResourceGroupConcurrency(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{
		{Name: "cpu", Workers: 1, QueueCapacity: 4},
		{Name: "network", Workers: 2, QueueCapacity: 4},
	})
	release := make(chan struct{})
	started := make(chan string, 4)
	execute := func(label string) ExecuteFunc {
		return func(ctx context.Context) error {
			started <- label
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	futures := submitAll(t, pool,
		Job{ID: "cpu-1", Group: "cpu", Execute: execute("cpu-1")},
		Job{ID: "cpu-2", Group: "cpu", Execute: execute("cpu-2")},
		Job{ID: "network-1", Group: "network", Execute: execute("network-1")},
		Job{ID: "network-2", Group: "network", Execute: execute("network-2")},
	)
	seen := map[string]bool{}
	for len(seen) < 3 {
		select {
		case label := <-started:
			seen[label] = true
		case <-time.After(time.Second):
			t.Fatalf("资源组未按独立配额启动: %v", seen)
		}
	}
	if !seen["cpu-1"] || !seen["network-1"] || !seen["network-2"] || seen["cpu-2"] {
		t.Fatalf("资源组隔离异常: %v", seen)
	}
	close(release)
	waitAll(t, futures)
}

func TestPoolUsesPriorityAndFIFOWithinGroup(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: "network", Workers: 1, QueueCapacity: 4}})
	blockStarted := make(chan struct{})
	release := make(chan struct{})
	blocker, err := pool.Submit(context.Background(), Job{ID: "blocker", Group: "network", Execute: func(ctx context.Context) error {
		close(blockStarted)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-blockStarted
	var mu sync.Mutex
	order := make([]string, 0, 3)
	job := func(id string, priority int) Job {
		return Job{ID: id, Group: "network", Priority: priority, Execute: func(context.Context) error {
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			return nil
		}}
	}
	futures := submitAll(t, pool, job("low", 1), job("high-first", 10), job("high-second", 10))
	close(release)
	waitAll(t, append([]*Future{blocker}, futures...))
	mu.Lock()
	defer mu.Unlock()
	if want := []string{"high-first", "high-second", "low"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("优先级或同优先级 FIFO 异常: want=%v got=%v", want, order)
	}
}

func TestPoolPreventsBackgroundJobStarvation(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: "network", Workers: 1, QueueCapacity: 8, MaxPriorityBurst: 2}})
	blockStarted := make(chan struct{})
	release := make(chan struct{})
	blocker, err := pool.Submit(context.Background(), Job{ID: "blocker", Group: "network", Priority: PriorityNormal, Execute: func(ctx context.Context) error {
		close(blockStarted)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-blockStarted
	var mu sync.Mutex
	order := make([]string, 0, 4)
	job := func(id string, priority int) Job {
		return Job{ID: id, Group: "network", Priority: priority, Execute: func(context.Context) error {
			mu.Lock()
			order = append(order, id)
			mu.Unlock()
			return nil
		}}
	}
	futures := submitAll(t, pool,
		job("background", PriorityBackground),
		job("interactive-1", PriorityInteractive),
		job("interactive-2", PriorityInteractive),
		job("interactive-3", PriorityInteractive),
	)
	close(release)
	waitAll(t, append([]*Future{blocker}, futures...))
	mu.Lock()
	defer mu.Unlock()
	backgroundIndex := -1
	for index, id := range order {
		if id == "background" {
			backgroundIndex = index
		}
	}
	if backgroundIndex < 0 || backgroundIndex > 2 {
		t.Fatalf("background 任务在高优先级洪峰中发生饥饿: order=%v", order)
	}
	if PriorityInteractive <= PriorityNormal || PriorityNormal <= PriorityBackground {
		t.Fatalf("标准优先级顺序无效: interactive=%d normal=%d background=%d", PriorityInteractive, PriorityNormal, PriorityBackground)
	}
}

func TestPoolRejectsFullQueueAndReportsMetrics(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: "browser", Workers: 1, QueueCapacity: 1}})
	started := make(chan struct{})
	release := make(chan struct{})
	running, err := pool.Submit(context.Background(), Job{ID: "running", Group: "browser", Execute: func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	queued, err := pool.Submit(context.Background(), Job{ID: "queued", Group: "browser", Execute: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Submit(context.Background(), Job{ID: "rejected", Group: "browser", Execute: func(context.Context) error { return nil }}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("队列满必须明确拒绝: %v", err)
	}
	snapshot := groupOf(t, pool.Snapshot(), "browser")
	if snapshot.Running != 1 || snapshot.Queued != 1 || snapshot.Rejected != 1 || snapshot.QueueCapacity != 1 || snapshot.MaxPriorityBurst != defaultMaxPriorityBurst {
		t.Fatalf("资源组指标异常: %#v", snapshot)
	}
	close(release)
	waitAll(t, []*Future{running, queued})
}

func TestPoolCancelsQueuedAndRunningJobs(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: "terminal", Workers: 1, QueueCapacity: 2}})
	started := make(chan struct{})
	running, err := pool.Submit(context.Background(), Job{ID: "running", Group: "terminal", Execute: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	queued, err := pool.Submit(context.Background(), Job{ID: "queued", Group: "terminal", Execute: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if !queued.Cancel() || queued.Cancel() {
		t.Fatal("排队任务取消必须仅成功一次")
	}
	if !running.Cancel() {
		t.Fatal("运行任务必须可取消")
	}
	for _, future := range []*Future{queued, running} {
		result, waitErr := future.Wait(context.Background())
		if waitErr != nil || !errors.Is(result.Err, ErrCanceled) {
			t.Fatalf("取消结果异常: result=%#v waitErr=%v", result, waitErr)
		}
	}
	snapshot := groupOf(t, pool.Snapshot(), "terminal")
	if snapshot.Canceled != 2 || snapshot.Queued != 0 || snapshot.Running != 0 {
		t.Fatalf("取消指标异常: %#v", snapshot)
	}
}

func TestPoolLeaseHoldsWorkerUntilReleased(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: GroupTerminal, Workers: 1, QueueCapacity: 2}})
	lease, err := pool.Acquire(context.Background(), LeaseRequest{
		ID: "terminal-session-1", Group: GroupTerminal, Priority: PriorityInteractive,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	queued, err := pool.Submit(context.Background(), Job{
		ID: "terminal-task", Group: GroupTerminal, Priority: PriorityNormal,
		Execute: func(context.Context) error { close(started); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("租约释放前不得让出资源组 Worker")
	case <-time.After(20 * time.Millisecond):
	}
	if snapshot := groupOf(t, pool.Snapshot(), GroupTerminal); snapshot.Running != 1 || snapshot.Queued != 1 {
		t.Fatalf("租约资源占用异常: %#v", snapshot)
	}
	if !lease.Release() || lease.Release() {
		t.Fatal("资源租约必须且只能释放一次")
	}
	if _, err := lease.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, err := queued.Wait(context.Background()); err != nil || result.Err != nil {
		t.Fatalf("租约后的任务执行失败: result=%#v err=%v", result, err)
	}
}

func TestPoolLeaseCancellationAndPoolCloseAreObservable(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: GroupTerminal, Workers: 1, QueueCapacity: 2}})
	first, err := pool.Acquire(context.Background(), LeaseRequest{ID: "first", Group: GroupTerminal})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, acquireErr := pool.Acquire(ctx, LeaseRequest{ID: "pending", Group: GroupTerminal})
		result <- acquireErr
	}()
	waitForPool(t, func() bool { return groupOf(t, pool.Snapshot(), GroupTerminal).Queued == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("排队租约取消错误 = %v", err)
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("资源池关闭后租约未撤销")
	}
	leaseResult, err := first.Wait(context.Background())
	if err != nil || !errors.Is(leaseResult.Err, ErrClosed) {
		t.Fatalf("资源池关闭后的租约结果异常: result=%#v err=%v", leaseResult, err)
	}
}

func TestPoolContainsPanicAndContinues(t *testing.T) {
	pool := newStartedPool(t, []GroupConfig{{Name: "media", Workers: 1, QueueCapacity: 2}})
	futures := submitAll(t, pool,
		Job{ID: "panic", Group: "media", Execute: func(context.Context) error { panic("sensitive") }},
		Job{ID: "normal", Group: "media", Execute: func(context.Context) error { return nil }},
	)
	first, err := futures[0].Wait(context.Background())
	if err != nil || !errors.Is(first.Err, ErrJobPanic) {
		t.Fatalf("panic 必须转换为稳定错误: result=%#v err=%v", first, err)
	}
	second, err := futures[1].Wait(context.Background())
	if err != nil || second.Err != nil {
		t.Fatalf("panic 后 Worker 必须继续: result=%#v err=%v", second, err)
	}
	if snapshot := groupOf(t, pool.Snapshot(), "media"); snapshot.Failed != 1 || snapshot.Completed != 1 {
		t.Fatalf("panic 指标异常: %#v", snapshot)
	}
}

func TestPoolValidatesLifecycleAndClosesDeterministically(t *testing.T) {
	if _, err := New(nil, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("空配置必须被拒绝: %v", err)
	}
	if _, err := New([]GroupConfig{{Name: "same", Workers: 1, QueueCapacity: 1}, {Name: "same", Workers: 1, QueueCapacity: 1}}, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("重复资源组必须被拒绝: %v", err)
	}
	if _, err := New([]GroupConfig{{Name: "cpu", Workers: 1, QueueCapacity: 1, MaxPriorityBurst: -1}}, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("非法防饥饿 burst 必须被拒绝: %v", err)
	}
	pool, err := New([]GroupConfig{{Name: "cpu", Workers: 1, QueueCapacity: 1}}, Options{Name: "resource-runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if pool.Name() != "resource-runtime" {
		t.Fatalf("生命周期名称未冻结: %q", pool.Name())
	}
	if _, err := pool.Submit(context.Background(), Job{ID: "before-start", Group: "cpu", Execute: func(context.Context) error { return nil }}); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("启动前必须拒绝提交: %v", err)
	}
	if err := pool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pool.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pool.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := pool.Stop(context.Background()); err != nil {
		t.Fatalf("重复停止必须幂等: %v", err)
	}
	if _, err := pool.Submit(context.Background(), Job{ID: "after-close", Group: "cpu", Execute: func(context.Context) error { return nil }}); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后必须拒绝提交: %v", err)
	}
}

func newStartedPool(t *testing.T, groups []GroupConfig) *Pool {
	t.Helper()
	pool, err := New(groups, Options{})
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
			t.Errorf("关闭资源池: %v", err)
		}
	})
	return pool
}

func submitAll(t *testing.T, pool *Pool, jobs ...Job) []*Future {
	t.Helper()
	futures := make([]*Future, 0, len(jobs))
	for _, job := range jobs {
		future, err := pool.Submit(context.Background(), job)
		if err != nil {
			t.Fatal(err)
		}
		futures = append(futures, future)
	}
	return futures
}

func waitAll(t *testing.T, futures []*Future) {
	t.Helper()
	for _, future := range futures {
		result, err := future.Wait(context.Background())
		if err != nil || result.Err != nil {
			t.Fatalf("任务执行失败: result=%#v err=%v", result, err)
		}
	}
}

func groupOf(t *testing.T, snapshot Snapshot, name Group) GroupSnapshot {
	t.Helper()
	for _, group := range snapshot.Groups {
		if group.Name == name {
			return group
		}
	}
	t.Fatalf("快照缺少资源组 %s: %#v", name, snapshot)
	return GroupSnapshot{}
}

func waitForPool(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待资源池状态超时")
}
