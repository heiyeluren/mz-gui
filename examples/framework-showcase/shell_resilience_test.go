package main

import (
	"context"
	"testing"
)

func TestProtectShowcaseShellAllowsClosedBreakerAndRetryBudget(t *testing.T) {
	if err := protectShowcaseShell(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProtectShowcaseShellRejectsNilContext(t *testing.T) {
	if err := protectShowcaseShell(nil); err == nil {
		t.Fatal("空上下文必须失败")
	}
}
