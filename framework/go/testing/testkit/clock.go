// Package testkit 提供 Framework 与应用测试可复用的确定性假件。
package testkit

import (
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type ManualClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*manualTimer
	tickers []*manualTicker
}

type manualTimer struct {
	at     time.Time
	values chan time.Time
}

type manualTicker struct {
	clock    *ManualClock
	interval time.Duration
	next     time.Time
	values   chan time.Time
	stopped  bool
}

func NewManualClock(now time.Time) *ManualClock {
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	return &ManualClock{now: now.UTC()}
}

func (clock *ManualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *ManualClock) After(duration time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	values := make(chan time.Time, 1)
	timer := &manualTimer{at: clock.now.Add(duration), values: values}
	clock.timers = append(clock.timers, timer)
	return values
}

func (clock *ManualClock) NewTicker(duration time.Duration) (frameworkclock.Ticker, error) {
	if duration <= 0 {
		return nil, frameworkclock.ErrInvalidDuration
	}
	clock.mu.Lock()
	defer clock.mu.Unlock()
	ticker := &manualTicker{clock: clock, interval: duration, next: clock.now.Add(duration), values: make(chan time.Time, 16)}
	clock.tickers = append(clock.tickers, ticker)
	return ticker, nil
}

func (clock *ManualClock) Advance(duration time.Duration) time.Time {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	now := clock.now
	pendingTimers := clock.timers[:0]
	readyTimers := make([]*manualTimer, 0)
	for _, timer := range clock.timers {
		if timer.at.After(now) {
			pendingTimers = append(pendingTimers, timer)
		} else {
			readyTimers = append(readyTimers, timer)
		}
	}
	clock.timers = pendingTimers
	tickers := append([]*manualTicker(nil), clock.tickers...)
	clock.mu.Unlock()

	for _, timer := range readyTimers {
		timer.values <- now
		close(timer.values)
	}
	for _, ticker := range tickers {
		ticker.fire(now)
	}
	return now
}

func (ticker *manualTicker) C() <-chan time.Time {
	return ticker.values
}

func (ticker *manualTicker) Stop() {
	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	if !ticker.stopped {
		ticker.stopped = true
		close(ticker.values)
	}
}

func (ticker *manualTicker) fire(now time.Time) {
	ticker.clock.mu.Lock()
	defer ticker.clock.mu.Unlock()
	for !ticker.stopped && !ticker.next.After(now) {
		select {
		case ticker.values <- ticker.next:
		default:
		}
		ticker.next = ticker.next.Add(ticker.interval)
	}
}
