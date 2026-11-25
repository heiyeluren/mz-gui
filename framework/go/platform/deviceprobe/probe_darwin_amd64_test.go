//go:build darwin && amd64

package deviceprobe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
	"github.com/heiyeluren/mz-gui/framework/go/observability/diagnostics"
	"github.com/heiyeluren/mz-gui/framework/go/security/leakscan"
	"github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestMacOSAMD64DeviceExercisesShippedFrameworkEntryPoints(t *testing.T) {
	if !IsMacOSAMD64() {
		t.Fatal("本测试只能在 darwin/amd64 真机执行")
	}
	report, err := Identify()
	if err != nil || report.GOOS != "darwin" || report.GOARCH != "amd64" {
		t.Fatalf("本机平台识别失败: %#v %v", report, err)
	}

	database, err := sqlite.Open(context.Background(), sqlite.Options{
		Path: filepath.Join(t.TempDir(), "device.db"),
		Migrations: fstest.MapFS{
			"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE device_probe (id TEXT PRIMARY KEY);")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// CGO_ENABLED=0 时 wails v3 pkg/mac 无可用文件，不能经 wailsapp 导入 application。
	store := windowstate.NewMemoryStore(windowstate.Options{})
	if err := store.Save(context.Background(), windowstate.Snapshot{
		WindowID: "win-device",
		RouteID:  "chat",
		Revision: 1,
		Payload:  []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "win-device")
	if err != nil || string(loaded.Payload) != `{"ok":true}` {
		t.Fatalf("窗口状态未在本机恢复: %#v %v", loaded, err)
	}

	if findings := leakscan.Scan("token=secret-value"); len(findings) == 0 {
		t.Fatal("本机 leakscan 未检出 token")
	}

	bundleDir := t.TempDir()
	result, err := diagnostics.Create(context.Background(), diagnostics.Options{
		OutputDirectory: bundleDir,
		FileName:        "device-diagnostics.zip",
		CreatedAt:       time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC),
		MaxFiles:        4,
		MaxFileBytes:    4096,
		MaxTotalBytes:   8192,
		Entries:         []diagnostics.Entry{{ArchiveName: "probe.txt", Data: []byte("darwin-amd64")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("本机诊断包未生成: %v", err)
	}

	if _, err := testkit.RunSoak(8, func(int) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
