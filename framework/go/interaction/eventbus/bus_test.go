package eventbus

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testEvent struct {
	Name string
}

func TestBusPublishesToIndependentSubscriptions(t *testing.T) {
	bus, err := New[testEvent](Options{Buffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	first, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	second, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Cancel()
	defer second.Cancel()

	want := testEvent{Name: "ready"}
	if err := bus.Publish(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	for index, channel := range []<-chan testEvent{first.Events(), second.Events()} {
		select {
		case got := <-channel:
			if got != want {
				t.Fatalf("订阅 %d 收到错误事件: %#v", index, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("订阅 %d 未收到事件", index)
		}
	}
}

func TestBusReportsBackpressureWithoutBlocking(t *testing.T) {
	bus, err := New[testEvent](Options{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	if err := bus.Publish(context.Background(), testEvent{Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(context.Background(), testEvent{Name: "second"}); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("满队列应返回 ErrBackpressure，实际为 %v", err)
	}
}

func TestBusCancelAndCloseAreSafe(t *testing.T) {
	bus, err := New[testEvent](Options{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	subscription.Cancel()
	subscription.Cancel()
	if _, ok := <-subscription.Events(); ok {
		t.Fatal("取消订阅后 channel 应关闭")
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bus.Close(); err != nil {
		t.Fatalf("重复 Close 应为幂等: %v", err)
	}
	if _, err := bus.Subscribe(); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后订阅应返回 ErrClosed，实际为 %v", err)
	}
	if err := bus.Publish(context.Background(), testEvent{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后发布应返回 ErrClosed，实际为 %v", err)
	}
}

func TestBusRejectsInvalidOptionsAndCancelledContext(t *testing.T) {
	if _, err := New[testEvent](Options{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("零缓冲应被拒绝，实际为 %v", err)
	}
	bus, err := New[testEvent](Options{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := bus.Publish(ctx, testEvent{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文应原样返回，实际为 %v", err)
	}
}
