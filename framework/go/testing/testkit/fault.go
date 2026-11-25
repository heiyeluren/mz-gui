package testkit

import (
	"errors"
	"sync"
)

var ErrFaultInjected = errors.New("测试故障已注入")

type FaultInjector struct {
	mu    sync.Mutex
	rules map[string]*faultRule
}

type faultRule struct {
	remaining int
	err       error
}

func NewFaultInjector() *FaultInjector {
	return &FaultInjector{rules: make(map[string]*faultRule)}
}

func (injector *FaultInjector) Fail(operation string, times int, err error) {
	if err == nil {
		err = ErrFaultInjected
	}
	injector.mu.Lock()
	defer injector.mu.Unlock()
	injector.rules[operation] = &faultRule{remaining: times, err: err}
}

func (injector *FaultInjector) Check(operation string) error {
	injector.mu.Lock()
	defer injector.mu.Unlock()
	rule := injector.rules[operation]
	if rule == nil || rule.remaining == 0 {
		return nil
	}
	if rule.remaining > 0 {
		rule.remaining--
	}
	return rule.err
}
