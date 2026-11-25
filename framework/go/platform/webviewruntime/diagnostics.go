package webviewruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/observability/diagnostics"
)

type RecoveryDiagnosticSpec struct {
	ProcessRole   string
	Platform      string
	BuildID       string
	SystemSummary string
	StackSummary  string
}

func CrashRecordFromRecoveryDecision(decision RecoveryDecision, spec RecoveryDiagnosticSpec) (diagnostics.CrashRecord, error) {
	if strings.TrimSpace(decision.WindowID) == "" || decision.Action == "" || decision.Reason == "" || decision.Attempt <= 0 || decision.OccurredAt.IsZero() {
		return diagnostics.CrashRecord{}, diagnostics.ErrInvalidCrashSpool
	}
	processRole := strings.TrimSpace(spec.ProcessRole)
	if processRole == "" {
		processRole = "desktop"
	}
	fingerprint, err := diagnostics.FingerprintCrash(
		"webview_crash",
		"webview",
		fmt.Sprintf("%s\n%s\n%s\n%s\n%s", processRole, strings.TrimSpace(spec.Platform), decision.WindowID, decision.Reason, decision.Action),
	)
	if err != nil {
		return diagnostics.CrashRecord{}, err
	}
	return diagnostics.CrashRecord{
		Fingerprint:    fingerprint,
		ProcessRole:    processRole,
		Platform:       strings.TrimSpace(spec.Platform),
		Component:      "webview",
		ErrorClass:     "webview_crash",
		ReasonCode:     string(decision.Reason),
		BuildID:        strings.TrimSpace(spec.BuildID),
		SystemSummary:  strings.TrimSpace(spec.SystemSummary),
		WebViewSummary: recoveryDecisionSummary(decision),
		StackSummary:   strings.TrimSpace(spec.StackSummary),
		Breadcrumbs: []diagnostics.Breadcrumb{
			{
				Name:         "webview.recovery.decision",
				PrivacyClass: diagnostics.PrivacyInternal,
				Attributes: map[string]string{
					"action":                      string(decision.Action),
					"attempt":                     fmt.Sprintf("%d", decision.Attempt),
					"diagnostic_mode":             fmt.Sprintf("%t", decision.DiagnosticMode),
					"preserve_window_identity":    fmt.Sprintf("%t", decision.PreserveWindowIdentity),
					"recreate_webview_allowed":    fmt.Sprintf("%t", decision.RecreateWebViewAllowed),
					"software_rendering":          fmt.Sprintf("%t", decision.SoftwareRendering),
					"consecutive_crash_count":     fmt.Sprintf("%d", decision.ConsecutiveCrashCount),
					"consecutive_crash_window_ms": fmt.Sprintf("%d", decision.ConsecutiveCrashWindow.Milliseconds()),
					"next_delay_ms":               fmt.Sprintf("%d", decision.NextDelay.Milliseconds()),
				},
			},
		},
		OccurredAt: decision.OccurredAt.UTC(),
	}, nil
}

func WriteRecoveryDecision(ctx context.Context, spool *diagnostics.CrashSpool, decision RecoveryDecision, spec RecoveryDiagnosticSpec) (diagnostics.CrashSpoolResult, error) {
	if ctx == nil || spool == nil {
		return diagnostics.CrashSpoolResult{}, diagnostics.ErrInvalidCrashSpool
	}
	record, err := CrashRecordFromRecoveryDecision(decision, spec)
	if err != nil {
		return diagnostics.CrashSpoolResult{}, err
	}
	return spool.Write(ctx, record)
}

func recoveryDecisionSummary(decision RecoveryDecision) string {
	return fmt.Sprintf(
		"window=%s action=%s reason=%s attempt=%d crash_count=%d diagnostic=%t software=%t recreate_allowed=%t",
		decision.WindowID,
		decision.Action,
		decision.Reason,
		decision.Attempt,
		decision.ConsecutiveCrashCount,
		decision.DiagnosticMode,
		decision.SoftwareRendering,
		decision.RecreateWebViewAllowed,
	)
}
