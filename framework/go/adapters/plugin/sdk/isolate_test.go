package sdk

import (
	"errors"
	"testing"
)

func TestIsolationGuardEnforcesResourceAndOrder(t *testing.T) {
	guard, err := NewIsolationGuard(1, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Admit(8); err != nil {
		t.Fatalf("首帧应准入: %v", err)
	}
	if err := guard.Admit(1); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("并发超限必须拒绝: %v", err)
	}
	guard.Release()
	if err := guard.Admit(32); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("字节超限必须拒绝: %v", err)
	}
	if err := guard.AdmitSequence(2); err != nil {
		t.Fatalf("首个序号应接受: %v", err)
	}
	if err := guard.AdmitSequence(2); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("乱序/重复必须拒绝: %v", err)
	}
	if err := guard.AdmitSequence(3); err != nil {
		t.Fatalf("递增序号应接受: %v", err)
	}
}

func TestIsolationGuardIsolatesAfterCrash(t *testing.T) {
	guard, err := NewIsolationGuard(2, 64)
	if err != nil {
		t.Fatal(err)
	}
	guard.ObserveCrash()
	if !guard.Isolated() {
		t.Fatal("崩溃后必须隔离")
	}
	if err := guard.Admit(1); !errors.Is(err, ErrPluginIsolated) {
		t.Fatalf("隔离后必须拒绝准入: %v", err)
	}
	if err := guard.AdmitSequence(1); !errors.Is(err, ErrPluginIsolated) {
		t.Fatalf("隔离后必须拒绝序号: %v", err)
	}
}

func TestIsolationGuardRejectsInvalidLimits(t *testing.T) {
	if _, err := NewIsolationGuard(0, 16); err == nil {
		t.Fatal("并发上限 0 必须拒绝")
	}
	if _, err := NewIsolationGuard(1, 0); err == nil {
		t.Fatal("字节上限 0 必须拒绝")
	}
}
