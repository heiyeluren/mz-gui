package sdk

import (
	"errors"
	"sync"
)

var ErrPluginDegraded = errors.New("插件已降级")

type DegradeGuard struct {
	threshold int

	mu       sync.Mutex
	failures int
	degraded bool
}

func NewDegradeGuard(threshold int) (*DegradeGuard, error) {
	if threshold < 1 || threshold > 64 {
		return nil, ErrProtocol
	}
	return &DegradeGuard{threshold: threshold}, nil
}

func (guard *DegradeGuard) Allow() error {
	if guard == nil {
		return ErrProtocol
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.degraded {
		return ErrPluginDegraded
	}
	return nil
}

func (guard *DegradeGuard) Observe(err error) {
	if guard == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if err == nil {
		guard.failures = 0
		guard.degraded = false
		return
	}
	guard.failures++
	if guard.failures >= guard.threshold {
		guard.degraded = true
	}
}
