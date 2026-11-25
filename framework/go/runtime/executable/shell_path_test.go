package executable

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoginShellSearchPathUsesOnlyPATHAndIgnoresNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	home := t.TempDir()
	shell := filepath.Join(home, "bash")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'profile noise\\n'\nprintf '\\000MZ_EXECUTABLE_PATH\\000/custom tools/bin:/usr/bin\\000'\nprintf 'later noise\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"SHELL=" + shell, "HOME=" + home, "PATH=/usr/bin:/bin", "MOZI_HOME=/frozen/engine", "TOKEN=fixture-secret"}
	before := append([]string(nil), env...)
	got, err := LoginShellSearchPath(context.Background(), ShellPathOptions{GOOS: "darwin", HomeDir: home, Environment: env})
	if err != nil || got != "/custom tools/bin:/usr/bin" {
		t.Fatalf("解析结果 %q %v", got, err)
	}
	if !reflect.DeepEqual(env, before) {
		t.Fatal("不能修改调用方环境快照")
	}
}

func TestLoginShellSearchPathLoadsZshUserConfiguration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("本机 zsh 登录配置")
	}
	home := t.TempDir()
	bin := filepath.Join(home, "用户工具")
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export PATH='"+bin+"':$PATH\nexport MOZI_HOME=/must-not-import\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoginShellSearchPath(context.Background(), ShellPathOptions{GOOS: "darwin", HomeDir: home, Environment: []string{"HOME=" + home, "ZDOTDIR=" + home, "SHELL=/bin/zsh", "PATH=/usr/bin:/bin"}})
	if err != nil || !strings.HasPrefix(got, bin+":") {
		t.Fatalf("没有加载用户 PATH: %q %v", got, err)
	}
}

func TestLoginShellSearchPathFailsBoundedly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	for _, test := range []struct {
		name, body string
		timeout    bool
	}{
		{"失败", "exit 1", false}, {"没有完整回执", "printf 'TOKEN=fixture-secret'", false},
		{"超时", "/bin/sleep 30", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			shell := filepath.Join(home, "bash")
			if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+test.body+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			got, err := LoginShellSearchPath(context.Background(), ShellPathOptions{GOOS: "linux", HomeDir: home, Environment: []string{"SHELL=" + shell}, Timeout: 40 * time.Millisecond})
			if err == nil || got != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("失败必须为空且不回显: %q %v", got, err)
			}
			if test.timeout && (!errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second) {
				t.Fatalf("超时没有有界终止: %v", err)
			}
		})
	}
}

func TestLoginShellSearchPathWindowsUsesNativeEnvironment(t *testing.T) {
	got, err := LoginShellSearchPath(context.Background(), ShellPathOptions{GOOS: "windows", HomeDir: t.TempDir(), Environment: []string{`Path=C:\custom\bin;C:\Windows`, "SHELL=/must-not-run"}})
	if err != nil || got != `C:\custom\bin;C:\Windows` {
		t.Fatalf("Windows 不应运行 POSIX shell: %q %v", got, err)
	}
}
