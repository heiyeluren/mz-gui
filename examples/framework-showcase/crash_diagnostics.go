package main

import (
	"fmt"
	"runtime"

	"github.com/heiyeluren/mz-gui/framework/go/observability/diagnostics"
)

func probeShowcaseCrashDiagnostics() error {
	platform := diagnostics.NormalizeCrashPlatform(runtime.GOOS, runtime.GOARCH)
	if platform == "" {
		return fmt.Errorf("showcase 崩溃平台无法归一化")
	}
	fingerprint, err := diagnostics.FingerprintCrash("startup.probe", "showcase-shell", "goroutine 1 [running]:\nshowcase.shell")
	if err != nil {
		return err
	}
	if fingerprint == "" {
		return fmt.Errorf("showcase 崩溃指纹为空")
	}
	fields := diagnostics.RedactFields(map[string]string{
		"platform":    platform,
		"fingerprint": fingerprint,
		"token":       "showcase-shell-probe",
		"note":        "see https://example.com/crash",
	})
	if fields["token"] != "[REDACTED]" || fields["note"] != "see [REDACTED_URL]" {
		return fmt.Errorf("showcase 诊断字段未脱敏")
	}
	return nil
}
