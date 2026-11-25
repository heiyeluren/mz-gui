package main

import (
	"context"
	"testing"
)

func TestProbeShowcaseStreamBoundaryRunsInterceptor(t *testing.T) {
	if err := probeShowcaseStreamBoundary(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProbeShowcaseStreamBoundaryRejectsNilContext(t *testing.T) {
	if err := probeShowcaseStreamBoundary(nil); err == nil {
		t.Fatal("空上下文必须失败")
	}
}
