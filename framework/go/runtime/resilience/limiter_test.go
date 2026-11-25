package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLimiterEnforcesCapacityAndRefillsDeterministically(t *testing.T) {
	clock := newTestClock()
	limiter, err := NewLimiter(LimiterConfig{
		Name: "network", Capacity: 5, RefillTokens: 2, RefillInterval: time.Second, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Acquire(5); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Acquire(1); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("容量耗尽错误 = %v", err)
	}
	clock.Advance(time.Second)
	if err := limiter.Acquire(2); err != nil {
		t.Fatal(err)
	}
	clock.Advance(10 * time.Second)
	if snapshot := limiter.Snapshot(); snapshot.Available != 5 || snapshot.Capacity != 5 {
		t.Fatalf("令牌桶未按容量封顶: %#v", snapshot)
	}
}

func TestLimiterWaitsWithClockAndSupportsCancellation(t *testing.T) {
	clock := newTestClock()
	limiter := mustLimiter(t, LimiterConfig{
		Name: "remote", Capacity: 1, RefillTokens: 1, RefillInterval: time.Second, Clock: clock,
	})
	if err := limiter.Acquire(1); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- limiter.Wait(context.Background(), 1) }()
	clock.WaitForTimer(t)
	clock.Advance(time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("等待者未在补充令牌后恢复")
	}

	if err := limiter.Acquire(1); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("令牌应已被等待者消费: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() { cancelled <- limiter.Wait(ctx, 1) }()
	clock.WaitForTimer(t)
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("等待取消错误 = %v", err)
	}
}

func TestLimiterConcurrentAcquireNeverExceedsCapacity(t *testing.T) {
	limiter := mustLimiter(t, LimiterConfig{Name: "cpu", Capacity: 10, RefillTokens: 1, RefillInterval: time.Hour})
	var acquired atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 100; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if limiter.Acquire(1) == nil {
				acquired.Add(1)
			}
		}()
	}
	group.Wait()
	if acquired.Load() != 10 {
		t.Fatalf("成功获取令牌数 = %d，期望 10", acquired.Load())
	}
}

func TestLimiterValidatesRequestsAndReportsRetryAfter(t *testing.T) {
	if _, err := NewLimiter(LimiterConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("非法配置错误 = %v", err)
	}
	clock := newTestClock()
	limiter := mustLimiter(t, LimiterConfig{
		Name: "terminal", Capacity: 3, RefillTokens: 1, RefillInterval: time.Second, Clock: clock,
	})
	if err := limiter.Acquire(0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("零令牌请求错误 = %v", err)
	}
	if err := limiter.Acquire(4); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("超容量请求错误 = %v", err)
	}
	if err := limiter.Acquire(3); err != nil {
		t.Fatal(err)
	}
	if delay, err := limiter.RetryAfter(2); err != nil || delay != 2*time.Second {
		t.Fatalf("重试时间 = %v err=%v", delay, err)
	}
	var nilContext context.Context
	if err := limiter.Wait(nilContext, 1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil context 错误 = %v", err)
	}
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	availableBefore := limiter.Snapshot().Available
	if err := limiter.Wait(cancelledContext, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("预先取消的 context 错误 = %v", err)
	}
	if availableAfter := limiter.Snapshot().Available; availableAfter != availableBefore {
		t.Fatalf("预先取消的等待消耗了令牌: before=%d after=%d", availableBefore, availableAfter)
	}
	if _, err := NewLimiter(LimiterConfig{
		Name: "overflow", Capacity: maximumLimiterCapacity, RefillTokens: 1, RefillInterval: maximumRefillInterval,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("不可表示的补充时长错误 = %v", err)
	}
}

func mustLimiter(t *testing.T, config LimiterConfig) *Limiter {
	t.Helper()
	value, err := NewLimiter(config)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
