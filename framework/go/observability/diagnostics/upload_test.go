package diagnostics

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/sampleauth"
)

func TestFlushCrashUploadsRequiresCapabilityAndDoesNotTouchSpool(t *testing.T) {
	root := writeCrash(t, "panic-runtime-001", time.Unix(1000, 0).UTC())
	var transport recordingTransport
	_, err := FlushCrashUploads(context.Background(), CrashUploadOptions{
		Directory: root, Capabilities: nil, Policy: sampleauth.Policy{Rate: 100},
		Transport: &transport, MaxAttempts: 3, InitialBackoff: time.Minute, MaxBackoff: time.Hour,
		MaxAge: 24 * time.Hour, Now: func() time.Time { return time.Unix(1100, 0).UTC() },
	})
	if !errors.Is(err, sampleauth.ErrUploadDenied) {
		t.Fatalf("无 capability 必须拒绝: %v", err)
	}
	if transport.calls != 0 {
		t.Fatal("未授权时不得调用上报通道")
	}
	if files, _ := filepath.Glob(filepath.Join(root, "*.json")); len(files) != 1 {
		t.Fatalf("未授权时不得删除 spool: %d", len(files))
	}
}

func TestFlushCrashUploadsSendsSampledRecordAndRemovesIt(t *testing.T) {
	root := writeCrash(t, "panic-runtime-001", time.Unix(1000, 0).UTC())
	var transport recordingTransport
	report, err := FlushCrashUploads(context.Background(), CrashUploadOptions{
		Directory: root, Capabilities: []string{"observability.upload.crashes"},
		Policy: sampleauth.Policy{Rate: 100}, Transport: &transport,
		MaxAttempts: 3, InitialBackoff: time.Minute, MaxBackoff: time.Hour,
		MaxAge: 24 * time.Hour, Now: func() time.Time { return time.Unix(1100, 0).UTC() },
	})
	if err != nil || report.Uploaded != 1 || report.Failed != 0 {
		t.Fatalf("上报结果错误: %#v err=%v", report, err)
	}
	if transport.calls != 1 || transport.last.Fingerprint != "panic-runtime-001" {
		t.Fatalf("上报载荷错误: %#v", transport.last)
	}
	if files, _ := filepath.Glob(filepath.Join(root, "*.json")); len(files) != 0 {
		t.Fatalf("成功上报后应删除记录: %v", files)
	}
}

func TestFlushCrashUploadsRetriesWithBackoffThenExpires(t *testing.T) {
	root := writeCrash(t, "panic-runtime-002", time.Unix(1000, 0).UTC())
	now := time.Unix(2000, 0).UTC()
	transport := &recordingTransport{err: errors.New("offline")}
	options := CrashUploadOptions{
		Directory: root, Capabilities: []string{"observability.upload.crashes"},
		Policy: sampleauth.Policy{Rate: 100}, Transport: transport,
		MaxAttempts: 2, InitialBackoff: time.Hour, MaxBackoff: 2 * time.Hour,
		MaxAge: 24 * time.Hour, Now: func() time.Time { return now },
	}
	report, err := FlushCrashUploads(context.Background(), options)
	if err != nil || report.Failed != 1 || report.Uploaded != 0 {
		t.Fatalf("首次失败应保留: %#v err=%v", report, err)
	}
	if files, _ := filepath.Glob(filepath.Join(root, "*.json")); len(files) != 1 {
		t.Fatal("失败后不得删除记录")
	}

	now = now.Add(time.Minute)
	options.Now = func() time.Time { return now }
	report, err = FlushCrashUploads(context.Background(), options)
	if err != nil || report.Failed != 0 || report.Skipped != 1 {
		t.Fatalf("退避窗口内不得重试: %#v err=%v", report, err)
	}
	if transport.calls != 1 {
		t.Fatalf("退避窗口内调用次数=%d", transport.calls)
	}

	now = now.Add(time.Hour)
	options.Now = func() time.Time { return now }
	report, err = FlushCrashUploads(context.Background(), options)
	if err != nil || report.Expired != 1 {
		t.Fatalf("达到最大重试应过期清理: %#v err=%v", report, err)
	}
	if files, _ := filepath.Glob(filepath.Join(root, "*.json")); len(files) != 0 {
		t.Fatalf("过期记录应删除: %v", files)
	}
}

func TestFlushCrashUploadsExpiresOldRecordsAndSkipsUnsampled(t *testing.T) {
	root := writeCrash(t, "panic-runtime-003", time.Unix(1, 0).UTC())
	var transport recordingTransport
	report, err := FlushCrashUploads(context.Background(), CrashUploadOptions{
		Directory: root, Capabilities: []string{"observability.upload.crashes"},
		Policy: sampleauth.Policy{Rate: 100}, Transport: &transport,
		MaxAttempts: 3, InitialBackoff: time.Minute, MaxBackoff: time.Hour,
		MaxAge: time.Hour, Now: func() time.Time { return time.Unix(4000, 0).UTC() },
	})
	if err != nil || report.Expired != 1 || transport.calls != 0 {
		t.Fatalf("超龄记录应清理且不上报: %#v calls=%d err=%v", report, transport.calls, err)
	}

	root = writeCrash(t, "panic-runtime-004", time.Unix(1000, 0).UTC())
	report, err = FlushCrashUploads(context.Background(), CrashUploadOptions{
		Directory: root, Capabilities: []string{"observability.upload.crashes"},
		Policy: sampleauth.Policy{Rate: 0}, Transport: &transport,
		MaxAttempts: 3, InitialBackoff: time.Minute, MaxBackoff: time.Hour,
		MaxAge: 24 * time.Hour, Now: func() time.Time { return time.Unix(1100, 0).UTC() },
	})
	if err != nil || report.Skipped != 1 || transport.calls != 0 {
		t.Fatalf("未采样不得上报: %#v calls=%d err=%v", report, transport.calls, err)
	}
	if files, _ := filepath.Glob(filepath.Join(root, "*.json")); len(files) != 1 {
		t.Fatal("未采样记录应保留")
	}
}

type recordingTransport struct {
	mu    sync.Mutex
	calls int
	last  CrashRecord
	err   error
}

func (transport *recordingTransport) Upload(_ context.Context, record CrashRecord, payload []byte) error {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.calls++
	transport.last = record
	if len(payload) == 0 {
		return errors.New("empty payload")
	}
	return transport.err
}

func writeCrash(t *testing.T, fingerprint string, occurred time.Time) string {
	t.Helper()
	root := t.TempDir()
	spool, err := NewCrashSpool(CrashSpoolOptions{Directory: root, MaxRecords: 8, MaxRecordBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spool.Write(context.Background(), CrashRecord{
		Fingerprint: fingerprint, ProcessRole: "desktop", Component: "webview",
		ErrorClass: "panic", ReasonCode: "renderer_process_gone", OccurredAt: occurred,
	}); err != nil {
		t.Fatal(err)
	}
	return root
}
