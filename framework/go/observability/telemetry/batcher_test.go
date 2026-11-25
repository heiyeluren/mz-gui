package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingSink struct {
	mu      sync.Mutex
	batches [][]Event
}

func (sink *recordingSink) Record(_ context.Context, events []Event) error {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.batches = append(sink.batches, append([]Event(nil), events...))
	return nil
}

func TestBatcherFlushesAtBatchLimitAndClose(t *testing.T) {
	sink := &recordingSink{}
	batcher, err := NewBatcher(context.Background(), sink, BatcherOptions{Capacity: 8, MaxBatch: 2, FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if !batcher.Submit(Event{ID: "event"}) {
			t.Fatal("事件不应被丢弃")
		}
	}
	if err := batcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if batcher.Submit(Event{ID: "late"}) {
		t.Fatal("关闭后的事件不应被接收")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.batches) != 2 || len(sink.batches[0]) != 2 || len(sink.batches[1]) != 1 {
		t.Fatalf("批次=%v", sink.batches)
	}
}

func TestBatcherRejectsInvalidOptions(t *testing.T) {
	if _, err := NewBatcher(context.Background(), &recordingSink{}, BatcherOptions{}); err != ErrInvalidBatcher {
		t.Fatalf("错误=%v", err)
	}
}

type channelSink struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (sink *channelSink) Record(_ context.Context, _ []Event) error {
	select {
	case sink.started <- struct{}{}:
	default:
	}
	if sink.release != nil {
		<-sink.release
	}
	return sink.err
}

func TestBatcherFlushesOnIntervalAndReportsSinkError(t *testing.T) {
	sinkErr := errors.New("写入失败")
	sink := &channelSink{started: make(chan struct{}, 1), err: sinkErr}
	errorChannel := make(chan error, 1)
	batcher, err := NewBatcher(context.Background(), sink, BatcherOptions{
		Capacity: 2, MaxBatch: 2, FlushInterval: 5 * time.Millisecond,
		OnError: func(err error) { errorChannel <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !batcher.Submit(Event{ID: "event"}) {
		t.Fatal("事件不应被丢弃")
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("定时刷新未触发")
	}
	select {
	case got := <-errorChannel:
		if !errors.Is(got, sinkErr) {
			t.Fatalf("错误=%v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("错误回调未触发")
	}
	if err := batcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBatcherDropCallbackMayCloseWithoutDeadlock(t *testing.T) {
	sink := &channelSink{started: make(chan struct{}, 1), release: make(chan struct{})}
	var batcher *Batcher
	dropped := make(chan struct{}, 1)
	var err error
	batcher, err = NewBatcher(context.Background(), sink, BatcherOptions{
		Capacity: 1, MaxBatch: 1, FlushInterval: time.Hour,
		OnDrop: func(Event) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			_ = batcher.Close(ctx)
			dropped <- struct{}{}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !batcher.Submit(Event{ID: "first"}) {
		t.Fatal("首个事件不应被丢弃")
	}
	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("首批未进入 sink")
	}
	if !batcher.Submit(Event{ID: "second"}) {
		t.Fatal("第二个事件应进入队列")
	}
	if batcher.Submit(Event{ID: "third"}) {
		t.Fatal("满载事件应被丢弃")
	}
	select {
	case <-dropped:
	case <-time.After(time.Second):
		t.Fatal("丢弃回调发生死锁")
	}
	close(sink.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := batcher.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := batcher.Close(nil); err != ErrInvalidBatcher {
		t.Fatalf("nil context 错误=%v", err)
	}
}
