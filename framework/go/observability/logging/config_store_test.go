package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/storage/configfile"
)

func TestConfigfileStoreFeedsLoggingDirectoryAndLevel(t *testing.T) {
	root := t.TempDir()
	configured := filepath.Join(root, "from-config")
	fallback := filepath.Join(root, "fallback")
	document := "[framework.logging]\n" +
		"directory = \"" + strings.ReplaceAll(configured, "\\", "\\\\") + "\"\n" +
		"level = \"warn\"\n" +
		"max_file_bytes = 4096\n"
	store := configfile.New(configfile.Options{
		Path: filepath.Join(root, "app.toml"), MaxBytes: 4096, Initial: []byte(document),
	})
	snapshot, err := store.LoadOrCreate()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := ParseSettings(snapshot.Data)
	if err != nil {
		t.Fatal(err)
	}
	options, err := ApplySettings(Options{ProcessName: "fw-core", Component: "logging"}, settings, fallback)
	if err != nil {
		t.Fatal(err)
	}
	if options.Directory != configured || options.Level != "warn" || options.MaxBytes != 4096 {
		t.Fatalf("配置未应用到日志选项: %#v", options)
	}
	logger, closer, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	logger.Warn("config-backed log")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(configured, "fw-core-*.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("配置目录未写入: %v err=%v", matches, err)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil || !strings.Contains(string(body), "config-backed log") {
		t.Fatalf("日志正文缺失: %s err=%v", body, err)
	}
}

func TestParseSettingsRejectsInvalidTOML(t *testing.T) {
	if _, err := ParseSettings([]byte("framework.logging = [")); err == nil {
		t.Fatal("非法 TOML 必须失败")
	}
}
