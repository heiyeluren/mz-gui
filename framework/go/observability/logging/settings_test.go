package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSettingsReadsFrameworkLoggingTable(t *testing.T) {
	settings, err := ParseSettings([]byte(`
schema_version = 1

[application]
name = "demo"

[framework.logging]
directory = "/var/log/mz-gui"
level = "warn"
max_file_bytes = 4096
`))
	if err != nil || settings.Directory != "/var/log/mz-gui" || settings.Level != "warn" || settings.MaxFileBytes != 4096 {
		t.Fatalf("解析结果错误: %#v err=%v", settings, err)
	}
}

func TestResolveDirectoryUsesDefaultAndRejectsRelativePath(t *testing.T) {
	defaultDir := filepath.Join(t.TempDir(), "logs")
	resolved, err := ResolveDirectory(Settings{}, defaultDir)
	if err != nil || resolved != defaultDir {
		t.Fatalf("空配置应回落默认目录: %s err=%v", resolved, err)
	}
	if _, err := ResolveDirectory(Settings{Directory: "relative/logs"}, defaultDir); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("相对目录应拒绝: %v", err)
	}
}

func TestApplySettingsOpensLoggerAtConfiguredDirectory(t *testing.T) {
	root := t.TempDir()
	configured := filepath.Join(root, "custom-logs")
	defaultDir := filepath.Join(root, "default-logs")
	document := []byte("[framework.logging]\ndirectory = \"" + strings.ReplaceAll(configured, "\\", "\\\\") + "\"\nlevel = \"info\"\nmax_file_bytes = 2048\n")
	settings, err := ParseSettings(document)
	if err != nil {
		t.Fatal(err)
	}
	options, err := ApplySettings(Options{ProcessName: "fw-agent", Component: "core"}, settings, defaultDir)
	if err != nil {
		t.Fatal(err)
	}
	logger, closer, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("framework started")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(configured, "fw-agent-*.jsonl"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("应写入配置目录: matches=%v err=%v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || !strings.Contains(string(data), "framework started") {
		t.Fatalf("日志内容缺失: %s err=%v", data, err)
	}
	if _, err := os.Stat(defaultDir); !os.IsNotExist(err) {
		t.Fatal("未配置时才应使用默认目录")
	}
}
