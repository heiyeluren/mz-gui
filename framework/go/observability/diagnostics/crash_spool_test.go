package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrashSpoolWritesRedactedRecordAndMaintainsCapacity(t *testing.T) {
	root := t.TempDir()
	spool, err := NewCrashSpool(CrashSpoolOptions{
		Directory: root, MaxRecords: 2, MaxRecordBytes: 4096,
		Now: func() time.Time { return time.Unix(1000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		_, err := spool.Write(context.Background(), CrashRecord{
			Fingerprint:    "panic-runtime-001",
			ProcessRole:    "desktop",
			Component:      "webview",
			ErrorClass:     "panic",
			ReasonCode:     "renderer_process_gone",
			BuildID:        "dev-build",
			SystemSummary:  "darwin arm64",
			WebViewSummary: "WKWebView current",
			StackSummary:   "panic: token=secret-value\nmain.func1",
			Breadcrumbs:    []Breadcrumb{{Name: "window.recovered", PrivacyClass: PrivacyInternal, Attributes: map[string]string{"path": "/Users/black/private.txt", "result": "ok"}}},
			OccurredAt:     time.Unix(int64(1000+index), 0).UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("crash spool 应按容量保留最新 2 条，实际=%d files=%#v", len(files), files)
	}
	data, err := os.ReadFile(files[len(files)-1])
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"secret-value", "/Users/black/private.txt"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("crash spool 泄露敏感内容 %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "panic-runtime-001") || !strings.Contains(text, "window.recovered") {
		t.Fatalf("crash spool 缺少安全诊断字段: %s", text)
	}
}

func TestDiagnosticManifestListsFilesAndRejectsSecrets(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "safe.log")
	if err := os.WriteFile(logPath, []byte(`{"level":"info","component":"runtime"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest(ManifestOptions{
		SchemaVersion: "mzgui.diagnostics.v1",
		Artifact:      "mz-gui-desktop",
		CreatedAt:     time.Unix(2000, 0).UTC(),
		Files:         []ManifestFile{{Name: "logs/safe.log", Path: logPath, PrivacyClass: PrivacyInternal}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "mzgui.diagnostics.v1" || manifest.Artifact != "mz-gui-desktop" || len(manifest.Files) != 1 {
		t.Fatalf("manifest 元数据错误: %#v", manifest)
	}
	if manifest.Files[0].SizeBytes == 0 || manifest.Files[0].SHA256 == "" || manifest.Files[0].PrivacyClass != PrivacyInternal {
		t.Fatalf("manifest 文件清单错误: %#v", manifest.Files[0])
	}

	secretPath := filepath.Join(root, "secret.log")
	if err := os.WriteFile(secretPath, []byte("authorization=Bearer abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = BuildManifest(ManifestOptions{
		SchemaVersion: "mzgui.diagnostics.v1",
		Artifact:      "mz-gui-desktop",
		CreatedAt:     time.Unix(2000, 0).UTC(),
		Files:         []ManifestFile{{Name: "logs/secret.log", Path: secretPath, PrivacyClass: PrivacySecret}},
	})
	if err == nil {
		t.Fatal("诊断 manifest 必须拒绝 secret 文件")
	}
}
