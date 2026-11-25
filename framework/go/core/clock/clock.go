package clock

import (
	"errors"
	"sync"
	"time"
)

var ErrInvalidDuration = errors.New("Framework 时钟间隔无效")

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	NewTicker(time.Duration) (Ticker, error)
}

type System struct{}

func (System) Now() time.Time {
	return time.Now().UTC()
}

func (System) After(duration time.Duration) <-chan time.Time {
	result := make(chan time.Time, 1)
	timer := time.NewTimer(duration)
	go func() {
		value := <-timer.C
		result <- value.UTC()
		close(result)
	}()
	return result
}

func (System) NewTicker(duration time.Duration) (Ticker, error) {
	if duration <= 0 {
		return nil, ErrInvalidDuration
	}
	ticker := &systemTicker{
		ticker: time.NewTicker(duration), values: make(chan time.Time, 1), done: make(chan struct{}),
	}
	go ticker.run()
	return ticker, nil
}

type systemTicker struct {
	ticker *time.Ticker
	values chan time.Time
	done   chan struct{}
	once   sync.Once
}

func (ticker *systemTicker) C() <-chan time.Time {
	return ticker.values
}

func (ticker *systemTicker) Stop() {
	ticker.once.Do(func() {
		ticker.ticker.Stop()
		close(ticker.done)
	})
}

func (ticker *systemTicker) run() {
	defer close(ticker.values)
	for {
		select {
		case value := <-ticker.ticker.C:
			select {
			case ticker.values <- value.UTC():
			default:
			}
		case <-ticker.done:
			return
		}
	}
}
