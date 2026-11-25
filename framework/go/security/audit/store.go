package audit

import (
	"context"
	"errors"
	"sync"
)

var ErrStoreFull = errors.New("审计存储已满")

type StoreOptions struct {
	MaxEvents int
}

type Store struct {
	mu      sync.Mutex
	max     int
	records []Record
}

type Appender interface {
	AppendEvent(context.Context, Event) (Record, error)
}

func NewStore(options StoreOptions) (*Store, error) {
	if options.MaxEvents < 1 || options.MaxEvents > 100000 {
		return nil, errors.New("审计存储容量无效")
	}
	return &Store{max: options.MaxEvents, records: make([]Record, 0, options.MaxEvents)}, nil
}

func (store *Store) Append(event Event) (Record, error) {
	if store == nil {
		return Record{}, errors.New("审计存储无效")
	}
	record, err := event.Record()
	if err != nil {
		return Record{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.records) >= store.max {
		return Record{}, ErrStoreFull
	}
	store.records = append(store.records, record)
	return record, nil
}

func (store *Store) AppendEvent(ctx context.Context, event Event) (Record, error) {
	if ctx == nil {
		return Record{}, errors.New("审计上下文无效")
	}
	select {
	case <-ctx.Done():
		return Record{}, ctx.Err()
	default:
	}
	return store.Append(event)
}

func (store *Store) List() []Record {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	cloned := make([]Record, len(store.records))
	copy(cloned, store.records)
	return cloned
}
