package managedprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestManagerStartsReportsAndStopsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 进程树由交叉编译与平台测试矩阵覆盖")
	}
	manager := New()
	snapshot, err := manager.Start(context.Background(), Spec{Executable: os.Args[0], Arguments: []string{"-test.run=TestManagedProcessHelper", "--"}, Environment: map[string]string{"MZ_GUI_PROCESS_HELPER": "1"}})
	if err != nil || !snapshot.Running || snapshot.PID < 1 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if manager.Status().Running || manager.Status().ExitedAt.IsZero() {
		t.Fatalf("status=%#v", manager.Status())
	}
	if err := manager.Stop(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("second stop=%v", err)
	}
}

func TestManagerRejectsUnsafeSpec(t *testing.T) {
	manager := New()
	if _, err := manager.Start(context.Background(), Spec{Executable: "relative"}); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("relative error=%v", err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err == nil {
		if _, err := manager.Start(context.Background(), Spec{Executable: link}); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("symlink error=%v", err)
		}
	}
}

func TestLinuxSidecarKeepsDesktopSessionWithoutProviderSecrets(t *testing.T) {
	values := map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus", "XDG_RUNTIME_DIR": "/run/user/1000", "DISPLAY": ":0", "WAYLAND_DISPLAY": "wayland-0", "XAUTHORITY": "/home/test/.Xauthority", "PROVIDER_API_KEY": "must-not-inherit"}
	got := mergeSidecarEnvironment("linux", func(key string) (string, bool) { value, ok := values[key]; return value, ok }, nil)
	joined := strings.Join(got, "\n")
	for key, value := range values {
		if key == "PROVIDER_API_KEY" {
			if strings.Contains(joined, value) {
				t.Fatal("不可继承 provider 密钥")
			}
			continue
		}
		if !strings.Contains(joined, key+"="+value) {
			t.Fatalf("Linux 桌面会话环境丢失: %s", key)
		}
	}
}

func TestManagedEnvironmentUsesAllowlistAndExplicitOverrides(t *testing.T) {
	t.Setenv("MZ_GUI_UNRELATED_SECRET", "must-not-leak")
	t.Setenv("PATH", "/safe/bin")
	values := mergedEnvironment(map[string]string{"MZ_GUI_GATEWAY_FEISHU_APP_SECRET": "process-secret"})
	joined := strings.Join(values, "\n")
	if strings.Contains(joined, "MZ_GUI_UNRELATED_SECRET") || !strings.Contains(joined, "PATH=/safe/bin") || !strings.Contains(joined, "MZ_GUI_GATEWAY_FEISHU_APP_SECRET=process-secret") {
		t.Fatalf("environment=%q", joined)
	}
}

func TestWindowsSidecarAllowlistIncludesLocalAppData(t *testing.T) {
	allowed := sidecarEnvAllowlist("windows")
	joined := strings.Join(allowed, ",")
	for _, key := range []string{"LOCALAPPDATA", "USERPROFILE", "PATH", "SystemRoot"} {
		found := false
		for _, item := range allowed {
			if item == key {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Windows sidecar allowlist 缺少 %s: %s", key, joined)
		}
	}
	lookup := func(key string) (string, bool) {
		switch key {
		case "PATH":
			return `C:\Windows\System32`, true
		case "USERPROFILE":
			return `C:\Users\tester`, true
		case "LOCALAPPDATA":
			return `C:\Users\tester\AppData\Local`, true
		case "MZ_GUI_UNRELATED_SECRET":
			return "must-not-leak", true
		default:
			return "", false
		}
	}
	env := mergeSidecarEnvironment("windows", lookup, nil)
	text := strings.Join(env, "\n")
	if !strings.Contains(text, "LOCALAPPDATA=C:\\Users\\tester\\AppData\\Local") {
		t.Fatalf("合并环境未包含 LOCALAPPDATA: %q", text)
	}
	if strings.Contains(text, "MZ_GUI_UNRELATED_SECRET") {
		t.Fatalf("秘密环境变量不得进入 sidecar: %q", text)
	}
}

func TestManagedProcessHelper(t *testing.T) {
	if os.Getenv("MZ_GUI_PROCESS_HELPER") != "1" {
		return
	}
	for {
		time.Sleep(time.Second)
	}
}
