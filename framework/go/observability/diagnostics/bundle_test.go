package diagnostics

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
	"time"
)

func TestCreateBuildsPrivateRedactedBundle(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "logs")
	output := filepath.Join(root, "artifacts")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logs, "mz-gui-agentd.jsonl")
	content := `{"level":"INFO","message":"request https://api.example.test/private from /Users/tester/private.txt","token":"secret-value","request_id":"request-1"}` + "\n" +
		`invalid prompt="private words with spaces" input='private command'` + "\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	result, err := Create(context.Background(), Options{
		OutputDirectory: output,
		FileName:        "mz-gui-diagnostics-test.zip",
		CreatedAt:       now,
		MaxFiles:        4,
		MaxFileBytes:    1 << 20,
		MaxTotalBytes:   2 << 20,
		Entries:         []Entry{{ArchiveName: "manifest.json", Data: []byte(`{"schema_version":1}`)}},
		Files:           []File{{ArchiveName: "logs/agentd.jsonl", Path: logPath}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FileName != "mz-gui-diagnostics-test.zip" || result.SizeBytes < 1 || result.SHA256 == "" || !result.CreatedAt.Equal(now) {
		t.Fatalf("结果=%+v", result)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("哈希=%s", result.SHA256)
	}
	testkit.AssertFileMode(t, result.Path, 0o600)
	reader, err := zip.OpenReader(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 2 {
		t.Fatalf("ZIP 条目=%d", len(reader.File))
	}
	var bundled string
	for _, item := range reader.File {
		if item.Name != "logs/agentd.jsonl" {
			continue
		}
		stream, openErr := item.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		payload, readErr := io.ReadAll(stream)
		_ = stream.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		bundled = string(payload)
	}
	for _, forbidden := range []string{"secret-value", "api.example.test", "/Users/tester", "private words", "private command"} {
		if strings.Contains(bundled, forbidden) {
			t.Fatalf("诊断日志泄露 %q: %s", forbidden, bundled)
		}
	}
	if !strings.Contains(bundled, "request-1") || !strings.Contains(bundled, "[REDACTED]") {
		t.Fatalf("诊断日志内容=%s", bundled)
	}
}

func TestCreateNeverOverwritesExistingBundle(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "bundle.zip")
	if err := os.WriteFile(target, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Create(context.Background(), Options{OutputDirectory: root, FileName: "bundle.zip", CreatedAt: time.Now().UTC(),
		MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024, Entries: []Entry{{ArchiveName: "manifest.json", Data: []byte("new")}}})
	if err == nil {
		t.Fatal("已有诊断包应拒绝覆盖")
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != "existing" {
		t.Fatalf("已有诊断包被修改: data=%q err=%v", data, readErr)
	}
}

func TestCreateRejectsUnsafeEntriesAndSources(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.log")
	if err := os.WriteFile(source, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.log")
	if err := os.Symlink(source, link); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}
	cases := []Options{
		{Entries: []Entry{{ArchiveName: "../escape", Data: []byte("x")}}},
		{Entries: []Entry{{ArchiveName: "same", Data: []byte("x")}, {ArchiveName: "same", Data: []byte("y")}}},
		{Files: []File{{ArchiveName: "logs/source.log", Path: link}}},
	}
	for index, options := range cases {
		options.OutputDirectory = filepath.Join(root, "output")
		options.FileName = "bundle.zip"
		options.CreatedAt = time.Now().UTC()
		options.MaxFiles = 4
		options.MaxFileBytes = 1024
		options.MaxTotalBytes = 2048
		if _, err := Create(context.Background(), options); err == nil {
			t.Fatalf("用例 %d 应拒绝", index)
		}
	}
}

func TestCreateHonorsBoundsAndCancellation(t *testing.T) {
	root := t.TempDir()
	base := Options{OutputDirectory: root, FileName: "bundle.zip", CreatedAt: time.Now().UTC(),
		MaxFiles: 1, MaxFileBytes: 4, MaxTotalBytes: 4}
	tooLarge := base
	tooLarge.Entries = []Entry{{ArchiveName: "manifest.json", Data: []byte("12345")}}
	if _, err := Create(context.Background(), tooLarge); err == nil {
		t.Fatal("超限条目应拒绝")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	base.Entries = []Entry{{ArchiveName: "manifest.json", Data: []byte("1234")}}
	if _, err := Create(cancelled, base); err != context.Canceled {
		t.Fatalf("取消错误=%v", err)
	}
}
