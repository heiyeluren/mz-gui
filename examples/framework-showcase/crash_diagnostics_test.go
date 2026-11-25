package main

import "testing"

func TestProbeShowcaseCrashDiagnosticsNormalizesAndFingerprints(t *testing.T) {
	if err := probeShowcaseCrashDiagnostics(); err != nil {
		t.Fatal(err)
	}
}
