package configfile

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

const DefaultWatchInterval = time.Second

type ApplyFunc func(Snapshot) error

type WatchOptions struct {
	Store    *Store
	Hub      *Hub
	Clock    clock.Clock
	Interval time.Duration
	Apply    ApplyFunc
}

type Watcher struct {
	store    *Store
	hub      *Hub
	clock    clock.Clock
	interval time.Duration
	apply    ApplyFunc

	mu        sync.Mutex
	lastGood  Snapshot
	lastError error
	started   bool
	cancel    context.CancelFunc
	ticker    clock.Ticker
}

func NewWatcher(options WatchOptions) (*Watcher, error) {
	if options.Store == nil {
		return nil, ErrInvalidOptions
	}
	if options.Interval <= 0 {
		options.Interval = DefaultWatchInterval
	}
	if options.Clock == nil {
		options.Clock = clock.System{}
	}
	snapshot, err := options.Store.LoadOrCreate()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		store: options.Store, hub: options.Hub, clock: options.Clock,
		interval: options.Interval, apply: options.Apply, lastGood: snapshot,
	}, nil
}

func (watcher *Watcher) LastSnapshot() Snapshot {
	if watcher == nil {
		return Snapshot{}
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	return Snapshot{Data: cloneBytes(watcher.lastGood.Data), Revision: watcher.lastGood.Revision}
}

func (watcher *Watcher) LastError() error {
	if watcher == nil {
		return nil
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	return watcher.lastError
}

func (watcher *Watcher) Poll() error {
	if watcher == nil || watcher.store == nil {
		return ErrInvalidOptions
	}
	if err := watcher.store.validate(); err != nil {
		return err
	}
	snapshot, err := LoadExisting(watcher.store.options.Path, watcher.store.options.MaxBytes)
	if err != nil {
		watcher.setError(err)
		return err
	}
	watcher.mu.Lock()
	current := watcher.lastGood
	watcher.mu.Unlock()
	if snapshot.Revision == current.Revision {
		watcher.setError(nil)
		return nil
	}
	if err := watcher.store.enforceGuards(current.Data, snapshot.Data); err != nil {
		watcher.setError(err)
		return err
	}
	if watcher.apply != nil {
		if err := watcher.apply(Snapshot{Data: cloneBytes(snapshot.Data), Revision: snapshot.Revision}); err != nil {
			applyErr := fmt.Errorf("配置热更新 apply 失败: %w", err)
			watcher.setError(applyErr)
			return applyErr
		}
	}
	watcher.mu.Lock()
	watcher.lastGood = Snapshot{Data: cloneBytes(snapshot.Data), Revision: snapshot.Revision}
	watcher.lastError = nil
	watcher.mu.Unlock()
	if watcher.hub != nil {
		watcher.hub.Publish(snapshot)
	}
	return nil
}

func (watcher *Watcher) Start(ctx context.Context) error {
	if watcher == nil || ctx == nil {
		return ErrInvalidOptions
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	if watcher.started {
		return ErrInvalidOptions
	}
	ticker, err := watcher.clock.NewTicker(watcher.interval)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	watcher.cancel = cancel
	watcher.ticker = ticker
	watcher.started = true
	go watcher.loop(runCtx, ticker)
	return nil
}

func (watcher *Watcher) Stop() {
	if watcher == nil {
		return
	}
	watcher.mu.Lock()
	cancel := watcher.cancel
	ticker := watcher.ticker
	watcher.cancel = nil
	watcher.ticker = nil
	watcher.started = false
	watcher.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ticker != nil {
		ticker.Stop()
	}
}

func (watcher *Watcher) loop(ctx context.Context, ticker clock.Ticker) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticker.C():
			if !ok {
				return
			}
			_ = watcher.Poll()
		}
	}
}

func (watcher *Watcher) setError(err error) {
	watcher.mu.Lock()
	watcher.lastError = err
	watcher.mu.Unlock()
}
