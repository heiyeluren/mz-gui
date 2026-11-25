package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreakerTransitionsClosedOpenHalfOpenAndClosed(t *testing.T) {
	clock := newTestClock()
	transitions := make([]Transition, 0, 3)
	breaker, err := NewBreaker(BreakerConfig{
		Name: "remote-api", FailureThreshold: 2, SuccessThreshold: 2,
		OpenTimeout: time.Minute, HalfOpenMaxRequests: 1, Clock: clock,
		OnTransition: func(value Transition) { transitions = append(transitions, value) },
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	first.Done(errors.New("temporary"))
	second, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	second.Done(errors.New("temporary"))
	if snapshot := breaker.Snapshot(); snapshot.State != StateOpen || snapshot.ConsecutiveFailures != 2 {
		t.Fatalf("熔断器未打开: %#v", snapshot)
	}
	if _, err := breaker.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("打开状态错误 = %v", err)
	}

	clock.Advance(time.Minute)
	probe, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := breaker.Allow(); !errors.Is(err, ErrHalfOpenSaturated) {
		t.Fatalf("半开探测配额错误 = %v", err)
	}
	probe.Done(nil)
	secondProbe, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	secondProbe.Done(nil)
	if snapshot := breaker.Snapshot(); snapshot.State != StateClosed || snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("熔断器未关闭: %#v", snapshot)
	}
	want := []State{StateOpen, StateHalfOpen, StateClosed}
	if len(transitions) != len(want) {
		t.Fatalf("状态迁移数量 = %d: %#v", len(transitions), transitions)
	}
	for index := range want {
		if transitions[index].To != want[index] {
			t.Fatalf("状态迁移[%d] = %#v", index, transitions[index])
		}
	}
}

func TestBreakerHalfOpenFailureReopensAndIgnoresStalePermit(t *testing.T) {
	clock := newTestClock()
	breaker := mustBreaker(t, BreakerConfig{
		Name: "storage", FailureThreshold: 1, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 2, Clock: clock,
	})
	permit, _ := breaker.Allow()
	permit.Done(errors.New("disk unavailable"))
	clock.Advance(time.Second)
	first, _ := breaker.Allow()
	stale, _ := breaker.Allow()
	first.Done(errors.New("still unavailable"))
	stale.Done(nil)
	if snapshot := breaker.Snapshot(); snapshot.State != StateOpen {
		t.Fatalf("过期 permit 不得关闭已重新打开的熔断器: %#v", snapshot)
	}
}

func TestBreakerExecuteClassifiesErrorsAndRecordsPanic(t *testing.T) {
	clock := newTestClock()
	breaker := mustBreaker(t, BreakerConfig{
		Name: "worker", FailureThreshold: 1, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 1, Clock: clock,
		IsFailure: func(err error) bool { return !errors.Is(err, context.Canceled) },
	})
	if err := breaker.Execute(context.Background(), func(context.Context) error { return context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatalf("执行错误 = %v", err)
	}
	if breaker.Snapshot().State != StateClosed {
		t.Fatal("调用方取消不应触发自定义失败分类")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("操作 panic 必须继续向上层传播")
			}
		}()
		_ = breaker.Execute(context.Background(), func(context.Context) error { panic("sensitive-value") })
	}()
	if snapshot := breaker.Snapshot(); snapshot.State != StateOpen {
		t.Fatalf("panic 必须计为失败: %#v", snapshot)
	}
}

func TestBreakerHalfOpenConcurrencyIsBounded(t *testing.T) {
	clock := newTestClock()
	breaker := mustBreaker(t, BreakerConfig{
		Name: "browser", FailureThreshold: 1, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 3, Clock: clock,
	})
	permit, _ := breaker.Allow()
	permit.Done(errors.New("offline"))
	clock.Advance(time.Second)
	var allowed atomic.Int32
	permits := make(chan *Permit, 20)
	var group sync.WaitGroup
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := breaker.Allow()
			if err == nil {
				allowed.Add(1)
				permits <- value
			}
		}()
	}
	group.Wait()
	close(permits)
	if allowed.Load() != 3 {
		t.Fatalf("半开并发 = %d，期望 3", allowed.Load())
	}
	for value := range permits {
		value.Done(nil)
	}
}

func TestBreakerValidatesConfigurationAndPermitCompletion(t *testing.T) {
	if _, err := NewBreaker(BreakerConfig{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("非法配置错误 = %v", err)
	}
	breaker := mustBreaker(t, BreakerConfig{
		Name: "valid", FailureThreshold: 2, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 1,
	})
	permit, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	permit.Done(errors.New("once"))
	permit.Done(errors.New("twice"))
	if breaker.Snapshot().ConsecutiveFailures != 1 {
		t.Fatal("permit 只能完成一次")
	}
	callbackPanic := mustBreaker(t, BreakerConfig{
		Name: "callback", FailureThreshold: 1, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 1,
		OnTransition: func(Transition) { panic("callback failure") },
	})
	callbackPermit, _ := callbackPanic.Allow()
	callbackPermit.Done(errors.New("open"))
	if callbackPanic.Snapshot().State != StateOpen {
		t.Fatal("状态回调 panic 不得破坏熔断状态")
	}
	if err := breaker.Execute(context.Background(), nil); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("nil operation 错误 = %v", err)
	}
}

func mustBreaker(t *testing.T, config BreakerConfig) *Breaker {
	t.Helper()
	value, err := NewBreaker(config)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
