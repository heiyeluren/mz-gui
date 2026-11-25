package inputmodel

import (
	"errors"
	"testing"
	"time"
)

func TestGateThrottlesSameKeyWithinWindow(t *testing.T) {
	gate, err := NewGate(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 19, 0, 0, 0, time.UTC)
	if !gate.Allow("save", now) {
		t.Fatal("首次应允许")
	}
	if gate.Allow("save", now.Add(200*time.Millisecond)) {
		t.Fatal("窗口内重复必须节流")
	}
	if !gate.Allow("other", now.Add(200*time.Millisecond)) {
		t.Fatal("不同 key 应允许")
	}
	if !gate.Allow("save", now.Add(time.Second)) {
		t.Fatal("窗口结束后应允许")
	}
}

func TestNewGateRejectsNonPositiveWindow(t *testing.T) {
	if _, err := NewGate(0); !errors.Is(err, ErrInvalidWindow) {
		t.Fatalf("零窗口错误 = %v", err)
	}
}
