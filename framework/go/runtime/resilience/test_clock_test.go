package resilience

import (
	"sync"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type testClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []testTimer
}

type testTimer struct {
	at     time.Time
	result chan time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) After(duration time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	result := make(chan time.Time, 1)
	clock.timers = append(clock.timers, testTimer{at: clock.now.Add(duration), result: result})
	return result
}

func (clock *testClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return nil, frameworkclock.ErrInvalidDuration
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	now := clock.now
	pending := clock.timers[:0]
	ready := make([]testTimer, 0, len(clock.timers))
	for _, timer := range clock.timers {
		if timer.at.After(now) {
			pending = append(pending, timer)
		} else {
			ready = append(ready, timer)
		}
	}
	clock.timers = pending
	clock.mu.Unlock()
	for _, timer := range ready {
		timer.result <- now
		close(timer.result)
	}
}

func (clock *testClock) WaitForTimer(t *testing.T) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		clock.mu.Lock()
		count := len(clock.timers)
		clock.mu.Unlock()
		if count > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("测试时钟未收到 timer")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
