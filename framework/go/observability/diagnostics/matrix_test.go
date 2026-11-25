package diagnostics

import (
	"context"
	"testing"
	"time"
)

func TestBuildCrashMatrixGroupsByFingerprint(t *testing.T) {
	root := t.TempDir()
	spool, err := NewCrashSpool(CrashSpoolOptions{
		Directory: root, MaxRecords: 8, MaxRecordBytes: 4096,
		Now: func() time.Time { return time.Unix(2000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	records := []CrashRecord{
		{Fingerprint: "panic-a", ProcessRole: "desktop", Component: "webview", ErrorClass: "panic", ReasonCode: "gone", OccurredAt: time.Unix(1000, 0).UTC()},
		{Fingerprint: "panic-a", ProcessRole: "desktop", Component: "webview", ErrorClass: "panic", ReasonCode: "gone", OccurredAt: time.Unix(1200, 0).UTC()},
		{Fingerprint: "fault-b", ProcessRole: "agentd", Component: "runtime", ErrorClass: "fault", ReasonCode: "timeout", OccurredAt: time.Unix(1100, 0).UTC()},
	}
	for _, record := range records {
		if _, err := spool.Write(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	matrix, err := BuildCrashMatrix(context.Background(), root)
	if err != nil || len(matrix) != 2 {
		t.Fatalf("崩溃矩阵行数错误: %#v %v", matrix, err)
	}
	if matrix[0].Fingerprint != "panic-a" || matrix[0].Count != 2 || matrix[0].LastOccurredAt.Unix() != 1200 {
		t.Fatalf("应按次数和最近时间聚合: %#v", matrix[0])
	}
	if matrix[1].Fingerprint != "fault-b" || matrix[1].Count != 1 {
		t.Fatalf("第二行错误: %#v", matrix[1])
	}
}

func TestBuildCrashMatrixSplitsSameFingerprintByPlatform(t *testing.T) {
	root := t.TempDir()
	spool, err := NewCrashSpool(CrashSpoolOptions{
		Directory: root, MaxRecords: 8, MaxRecordBytes: 4096,
		Now: func() time.Time { return time.Unix(3000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	records := []CrashRecord{
		{Fingerprint: "panic-a", ProcessRole: "desktop", Platform: "macos-arm64", Component: "webview", ErrorClass: "panic", ReasonCode: "gone", OccurredAt: time.Unix(1000, 0).UTC()},
		{Fingerprint: "panic-a", ProcessRole: "desktop", Platform: "windows-x64", Component: "webview", ErrorClass: "panic", ReasonCode: "gone", OccurredAt: time.Unix(1100, 0).UTC()},
	}
	for _, record := range records {
		if _, err := spool.Write(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	matrix, err := BuildCrashMatrix(context.Background(), root)
	if err != nil || len(matrix) != 2 {
		t.Fatalf("同指纹不同平台必须分行: %#v %v", matrix, err)
	}
	if matrix[0].Platform == matrix[1].Platform || matrix[0].Fingerprint != "panic-a" || matrix[1].Fingerprint != "panic-a" {
		t.Fatalf("平台分行异常: %#v", matrix)
	}
}

func TestNormalizeCrashPlatformMapsLocalGOOS(t *testing.T) {
	if got := NormalizeCrashPlatform("darwin", "arm64"); got != "macos-arm64" {
		t.Fatalf("darwin/arm64 = %q", got)
	}
	if got := NormalizeCrashPlatform("windows", "amd64"); got != "windows-x64" {
		t.Fatalf("windows/amd64 = %q", got)
	}
	if got := NormalizeCrashPlatform("", ""); got != "" {
		t.Fatalf("空平台必须为空: %q", got)
	}
}
