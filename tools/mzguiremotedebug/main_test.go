package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/platform/remotedebug"
)

func TestRunRequiresHost(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"run"}, &stdout, &stderr); code != 2 {
		t.Fatalf("缺少 host 应返回 2: %d %s", code, stderr.String())
	}
}

func TestRunAndGetAgainstLocalServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("hello-remote"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &remotedebug.Server{
		Addr:     "127.0.0.1:0",
		Password: remotedebug.DefaultPassword,
		Runner: func(_ context.Context, command string) remotedebug.Result {
			return remotedebug.Result{ExitCode: 0, Stdout: "ok:" + command}
		},
	}
	addr, stop, err := server.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	host, port, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("地址无效: %s", addr)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"run", "--host", host, "--port", port, "--timeout", "5s", "whoami"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run 失败: %d %s", code, stderr.String())
	}
	if stdout.String() != "ok:whoami" {
		t.Fatalf("run 输出错误: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"get", "--host", host, "--port", port, file}, &stdout, &stderr); code != 0 {
		t.Fatalf("get 失败: %d %s", code, stderr.String())
	}
	if stdout.String() != "hello-remote" {
		t.Fatalf("get 输出错误: %q", stdout.String())
	}
}
