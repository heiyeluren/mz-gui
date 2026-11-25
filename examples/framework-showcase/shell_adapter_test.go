package main

import (
	"context"
	"testing"
)

func TestProbeShowcaseAdaptersValidatesAndServesHandshake(t *testing.T) {
	if err := probeShowcaseAdapters(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProbeShowcaseAdaptersRejectsNilContext(t *testing.T) {
	if err := probeShowcaseAdapters(nil); err == nil {
		t.Fatal("空上下文必须失败")
	}
}

func TestRunShowcasePluginContractAcceptsChild(t *testing.T) {
	runShowcasePluginContract(t)
}
