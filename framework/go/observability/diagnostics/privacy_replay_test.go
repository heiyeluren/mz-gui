package diagnostics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// W115-G684：结构化日志/指标/trace/crash spool 隐私回读。不编造事件。诊断包见 T137。

func TestReplayCrashSpoolDoesNotFabricateEvents(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	records, err := ReplayCrashSpool(context.Background(), missing)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("空目录不得编造崩溃事件: %#v", records)
	}

	empty := filepath.Join(t.TempDir(), "empty-spool")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	records, err = ReplayCrashSpool(context.Background(), empty)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("无记录时不得编造崩溃事件: %#v", records)
	}
}

func TestReplayCrashSpoolRedactsOnReadBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "crash-spool")
	spool, err := NewCrashSpool(CrashSpoolOptions{
		Directory: root, MaxRecords: 8, MaxRecordBytes: 8192,
		Now: func() time.Time { return time.Unix(1700, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = spool.Write(context.Background(), CrashRecord{
		Fingerprint:   "panic-runtime-g684",
		ProcessRole:   "agentd",
		Component:     "runtime",
		ErrorClass:    "panic",
		ReasonCode:    "unhandled_panic",
		StackSummary:  "panic: token=must-not-appear\nmain.func1",
		SystemSummary: "darwin arm64",
		Breadcrumbs:   []Breadcrumb{{Name: "task.failed", PrivacyClass: PrivacyInternal, Attributes: map[string]string{"path": "/Users/tester/private.txt", "result": "fail"}}},
		OccurredAt:    time.Unix(1700, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	records, err := ReplayCrashSpool(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("应回读 1 条真实记录，实际=%d", len(records))
	}
	payload, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"must-not-appear", "/Users/tester/private.txt"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("crash spool 回读泄露 %q: %s", forbidden, text)
		}
	}
	if records[0].Fingerprint != "panic-runtime-g684" || records[0].ReasonCode != "unhandled_panic" {
		t.Fatalf("回读丢掉安全诊断字段: %#v", records[0])
	}
}

func TestReplayCrashSpoolSecondPassRedactsLeftoverSecrets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "legacy-spool")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{
  "schema_version": "mzgui.crash.v1",
  "fingerprint": "legacy-panic-001",
  "process_role": "desktop",
  "component": "webview",
  "error_class": "panic",
  "reason_code": "renderer_process_gone",
  "stack_summary": "token=legacy-secret-value authorization=Bearer abc.def",
  "occurred_at": "2026-08-25T00:00:00Z"
}`)
	if err := os.WriteFile(filepath.Join(root, "20260825T000000Z-legacy.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := ReplayCrashSpool(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("应回读遗留记录，实际=%d", len(records))
	}
	payload, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"legacy-secret-value", "abc.def", "Bearer abc"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("二次脱敏未清掉遗留秘密 %q: %s", forbidden, text)
		}
	}
	if records[0].Fingerprint != "legacy-panic-001" {
		t.Fatalf("二次脱敏不得丢掉指纹: %#v", records[0])
	}
}

func TestReplayLogPrivacyDoesNotFabricateLines(t *testing.T) {
	if got := ReplayLogPrivacy(nil); len(got) != 0 {
		t.Fatalf("空日志不得编造行: %q", got)
	}
	if got := ReplayLogPrivacy([]byte("   \n")); len(bytesTrimForReplay(got)) != 0 && string(got) != "   \n" && strings.TrimSpace(string(got)) != "" {
		t.Fatalf("空白日志回读不得编造事件: %q", got)
	}
	raw := []byte(`{"level":"info","message":"ok","token":"must-not-appear","url":"https://example.test/private"}` + "\n")
	replayed := ReplayLogPrivacy(raw)
	text := string(replayed)
	for _, forbidden := range []string{"must-not-appear", "https://example.test/private"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("日志回读泄露 %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"level":"info"`) && !strings.Contains(text, `"level": "info"`) {
		t.Fatalf("日志回读应保留非敏感字段: %s", text)
	}
}

func TestReplayCrashSpoolRejectsRelativeDirectory(t *testing.T) {
	if _, err := ReplayCrashSpool(context.Background(), "relative-spool"); err == nil {
		t.Fatal("相对路径不得回读")
	}
	if _, err := ReplayCrashSpool(nil, filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("空 context 不得回读")
	}
}

func bytesTrimForReplay(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
}
