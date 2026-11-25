package sdk

import (
	"errors"
	"testing"
)

func TestDegradeGuardIsolatesAfterConsecutiveFailures(t *testing.T) {
	guard, err := NewDegradeGuard(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Allow(); err != nil {
		t.Fatalf("初始应允许调用: %v", err)
	}
	guard.Observe(errors.New("handshake failed"))
	if err := guard.Allow(); err != nil {
		t.Fatalf("未达阈值仍应允许: %v", err)
	}
	guard.Observe(errors.New("handshake failed"))
	if err := guard.Allow(); !errors.Is(err, ErrPluginDegraded) {
		t.Fatalf("连续失败后必须降级: %v", err)
	}
	guard.Observe(nil)
	if err := guard.Allow(); err != nil {
		t.Fatalf("成功应解除降级: %v", err)
	}
}

func TestDegradeGuardRejectsInvalidThreshold(t *testing.T) {
	if _, err := NewDegradeGuard(0); err == nil {
		t.Fatal("阈值 0 必须拒绝")
	}
}
