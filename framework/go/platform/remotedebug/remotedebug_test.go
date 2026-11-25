package remotedebug

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPortAndPasswordAreFixed(t *testing.T) {
	if DefaultPort != 1983 {
		t.Fatalf("端口必须写死 1983，实际 %d", DefaultPort)
	}
	if DefaultPassword == "" {
		t.Fatal("必须有固定配对密码")
	}
}

func TestDialRejectsWrongPasswordAndRunsAfterAuth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := &Server{
		Addr:     "127.0.0.1:0",
		Password: DefaultPassword,
		Runner: func(_ context.Context, command string) Result {
			if command != "whoami" {
				return Result{ExitCode: 1, Stderr: "unexpected " + command}
			}
			return Result{ExitCode: 0, Stdout: "heiye\r\n"}
		},
	}
	addr, stop, err := server.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if _, err := Dial(ctx, addr, "wrong-password"); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("错误密码应失败: %v", err)
	}
	client, err := Dial(ctx, addr, DefaultPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.Run(ctx, "whoami", time.Second)
	if err != nil || result.ExitCode != 0 || strings.TrimSpace(result.Stdout) != "heiye" {
		t.Fatalf("配对后应能执行命令: %#v %v", result, err)
	}
}

func TestClientGetsRemoteFileAfterAuth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(file, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{Addr: "127.0.0.1:0", Password: DefaultPassword}
	addr, stop, err := server.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client, err := Dial(ctx, addr, DefaultPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	data, err := client.Get(ctx, file)
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("应读到远端文件: %q %v", data, err)
	}
}

func TestRunCheckedPropagatesRemoteExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server := &Server{
		Addr:     "127.0.0.1:0",
		Password: DefaultPassword,
		Runner: func(_ context.Context, _ string) Result {
			return Result{ExitCode: 23, Stdout: "report.json", Stderr: "route failed"}
		},
	}
	addr, stop, err := server.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client, err := Dial(ctx, addr, DefaultPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.RunChecked(ctx, "mzgui-native-e2e", time.Second)
	if !errors.Is(err, ErrRemoteCommandFailed) || result.ExitCode != 23 || result.Stdout != "report.json" {
		t.Fatalf("远端非零退出必须透传: result=%#v err=%v", result, err)
	}
}
