package sdk

import (
	"errors"
	"sync"
)

var (
	ErrPluginIsolated = errors.New("插件已隔离")
	ErrResourceLimit  = errors.New("插件资源超限")
	ErrOutOfOrder     = errors.New("插件帧乱序")
)

type IsolationGuard struct {
	maxInflight int
	maxBytes    int

	mu       sync.Mutex
	inflight int
	isolated bool
	lastSeq  uint64
	hasSeq   bool
}

func NewIsolationGuard(maxInflight, maxBytes int) (*IsolationGuard, error) {
	if maxInflight < 1 || maxInflight > 256 || maxBytes < 1 {
		return nil, ErrProtocol
	}
	return &IsolationGuard{maxInflight: maxInflight, maxBytes: maxBytes}, nil
}

func (guard *IsolationGuard) Admit(bytes int) error {
	if guard == nil || bytes < 0 {
		return ErrProtocol
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.isolated {
		return ErrPluginIsolated
	}
	if bytes > guard.maxBytes || guard.inflight >= guard.maxInflight {
		return ErrResourceLimit
	}
	guard.inflight++
	return nil
}

func (guard *IsolationGuard) Release() {
	if guard == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.inflight > 0 {
		guard.inflight--
	}
}

func (guard *IsolationGuard) AdmitSequence(seq uint64) error {
	if guard == nil {
		return ErrProtocol
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.isolated {
		return ErrPluginIsolated
	}
	if guard.hasSeq && seq <= guard.lastSeq {
		return ErrOutOfOrder
	}
	guard.lastSeq = seq
	guard.hasSeq = true
	return nil
}

func (guard *IsolationGuard) ObserveCrash() {
	if guard == nil {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.isolated = true
	guard.inflight = 0
}

func (guard *IsolationGuard) Isolated() bool {
	if guard == nil {
		return false
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	return guard.isolated
}
