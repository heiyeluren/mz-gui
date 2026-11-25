package eventbus

import (
	"context"
	"errors"
	"sync"
)

const (
	defaultMaxSubscribers = 64
	maxBuffer             = 4096
)

var (
	ErrInvalidArgument = errors.New("事件总线参数无效")
	ErrBackpressure    = errors.New("事件订阅者处理速度不足")
	ErrClosed          = errors.New("事件总线已关闭")
	ErrLimitReached    = errors.New("事件订阅数量已达上限")
)

type Options struct {
	Buffer         int
	MaxSubscribers int
}

type Bus[T any] struct {
	mu             sync.RWMutex
	buffer         int
	maxSubscribers int
	nextID         uint64
	closed         bool
	subscriptions  map[uint64]*Subscription[T]
}

type Subscription[T any] struct {
	bus    *Bus[T]
	id     uint64
	events chan T
	once   sync.Once
}

func New[T any](options Options) (*Bus[T], error) {
	if options.Buffer < 1 || options.Buffer > maxBuffer || options.MaxSubscribers < 0 {
		return nil, ErrInvalidArgument
	}
	maxSubscribers := options.MaxSubscribers
	if maxSubscribers == 0 {
		maxSubscribers = defaultMaxSubscribers
	}
	if maxSubscribers > 4096 {
		return nil, ErrInvalidArgument
	}
	return &Bus[T]{
		buffer: options.Buffer, maxSubscribers: maxSubscribers,
		subscriptions: make(map[uint64]*Subscription[T]),
	}, nil
}

func (bus *Bus[T]) Subscribe() (*Subscription[T], error) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return nil, ErrClosed
	}
	if len(bus.subscriptions) >= bus.maxSubscribers {
		return nil, ErrLimitReached
	}
	bus.nextID++
	subscription := &Subscription[T]{bus: bus, id: bus.nextID, events: make(chan T, bus.buffer)}
	bus.subscriptions[subscription.id] = subscription
	return subscription, nil
}

func (bus *Bus[T]) Publish(ctx context.Context, event T) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	if bus.closed {
		return ErrClosed
	}
	backpressure := false
	for _, subscription := range bus.subscriptions {
		select {
		case subscription.events <- event:
		case <-ctx.Done():
			return ctx.Err()
		default:
			backpressure = true
		}
	}
	if backpressure {
		return ErrBackpressure
	}
	return nil
}

func (bus *Bus[T]) Close() error {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if bus.closed {
		return nil
	}
	bus.closed = true
	for id, subscription := range bus.subscriptions {
		delete(bus.subscriptions, id)
		close(subscription.events)
	}
	return nil
}

func (subscription *Subscription[T]) Events() <-chan T {
	return subscription.events
}

func (subscription *Subscription[T]) Cancel() {
	subscription.once.Do(func() {
		bus := subscription.bus
		bus.mu.Lock()
		defer bus.mu.Unlock()
		if _, exists := bus.subscriptions[subscription.id]; !exists {
			return
		}
		delete(bus.subscriptions, subscription.id)
		close(subscription.events)
	})
}
