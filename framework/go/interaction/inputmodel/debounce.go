package inputmodel

import (
	"errors"
	"sync"
	"time"
)

var ErrInvalidWindow = errors.New("输入窗口参数无效")

type Gate struct {
	window time.Duration

	mu      sync.Mutex
	last    map[string]time.Time
}

func NewGate(window time.Duration) (*Gate, error) {
	if window <= 0 {
		return nil, ErrInvalidWindow
	}
	return &Gate{window: window, last: make(map[string]time.Time)}, nil
}

func (gate *Gate) Allow(key string, now time.Time) bool {
	if gate == nil || key == "" || now.IsZero() {
		return false
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	previous, exists := gate.last[key]
	if exists && !now.Before(previous) && now.Sub(previous) < gate.window {
		return false
	}
	gate.last[key] = now
	return true
}
