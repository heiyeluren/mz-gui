package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrInvalidBatcher = errors.New("语义事件批处理器配置无效")

type Sink interface {
	Record(context.Context, []Event) error
}

type BatcherOptions struct {
	Capacity      int
	MaxBatch      int
	FlushInterval time.Duration
	OnDrop        func(Event)
	OnError       func(error)
}

type Batcher struct {
	sink   Sink
	queue  chan Event
	done   chan struct{}
	cancel context.CancelFunc
	onDrop func(Event)
	onErr  func(error)
	max    int
	once   sync.Once
	mu     sync.RWMutex
	closed bool
}

func NewBatcher(parent context.Context, sink Sink, options BatcherOptions) (*Batcher, error) {
	if parent == nil || sink == nil || options.Capacity < 1 || options.MaxBatch < 1 || options.MaxBatch > MaxBatchSize ||
		options.Capacity < options.MaxBatch || options.FlushInterval <= 0 {
		return nil, ErrInvalidBatcher
	}
	ctx, cancel := context.WithCancel(parent)
	batcher := &Batcher{sink: sink, queue: make(chan Event, options.Capacity), done: make(chan struct{}), cancel: cancel,
		onDrop: options.OnDrop, onErr: options.OnError, max: options.MaxBatch}
	go batcher.run(ctx, options.FlushInterval)
	return batcher, nil
}

func (batcher *Batcher) Submit(event Event) bool {
	batcher.mu.RLock()
	if batcher.closed {
		batcher.mu.RUnlock()
		return false
	}
	accepted := false
	select {
	case batcher.queue <- event:
		accepted = true
	default:
	}
	batcher.mu.RUnlock()
	if !accepted && batcher.onDrop != nil {
		batcher.onDrop(event)
	}
	return accepted
}

func (batcher *Batcher) Close(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidBatcher
	}
	batcher.once.Do(func() {
		batcher.mu.Lock()
		batcher.closed = true
		batcher.cancel()
		batcher.mu.Unlock()
	})
	select {
	case <-batcher.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (batcher *Batcher) run(ctx context.Context, interval time.Duration) {
	defer close(batcher.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	batch := make([]Event, 0, batcher.max)
	flush := func(flushCtx context.Context) {
		if len(batch) == 0 {
			return
		}
		payload := append([]Event(nil), batch...)
		batch = batch[:0]
		if err := batcher.sink.Record(flushCtx, payload); err != nil && batcher.onErr != nil {
			batcher.onErr(err)
		}
	}
	for {
		select {
		case event := <-batcher.queue:
			batch = append(batch, event)
			if len(batch) >= batcher.max {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			for {
				select {
				case event := <-batcher.queue:
					batch = append(batch, event)
					if len(batch) >= batcher.max {
						flush(context.Background())
					}
				default:
					flush(context.Background())
					return
				}
			}
		}
	}
}
