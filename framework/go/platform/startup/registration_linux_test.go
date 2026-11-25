//go:build linux

package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type linuxRunner struct {
	calls []string
	fail  bool
}

func (r *linuxRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.fail {
		return nil, errors.New("systemd unavailable")
	}
	return []byte("enabled\n"), nil
}

func TestLinuxRegistrationPreservesInstanceEnvironmentAndReportsFailure(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "worker")
	if err := os.WriteFile(executable, []byte("worker"), 0700); err != nil {
		t.Fatal(err)
	}
	runner := &linuxRunner{}
	registration := NewLinuxRegistration(LinuxRegistrationOptions{Unit: "test-worker.service", Directory: filepath.Join(root, "units"), Executable: executable, Environment: map[string]string{"MOZI_HOME": "/data/engine 2", "MOZI_CACHE": "/cache/engine 2"}, Runner: runner})
	if err := registration.SetEnabled(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	status, err := registration.Status(context.Background())
	if err != nil || !status.Enabled || status.Drifted {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	data, _ := os.ReadFile(status.Location)
	for _, value := range []string{"Environment=\"MOZI_HOME=/data/engine 2\"", "Environment=\"MOZI_CACHE=/cache/engine 2\""} {
		if !strings.Contains(string(data), value) {
			t.Fatalf("缺失实例环境: %s", data)
		}
	}
	runner.fail = true
	if err := registration.SetEnabled(context.Background(), false); err == nil {
		t.Fatal("systemctl 失败不得伪装停用成功")
	}
	if _, err := os.Stat(status.Location); err != nil {
		t.Fatal("停用失败不得删除服务文件")
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "launchctl") {
			t.Fatal("Linux 禁止 launchd")
		}
	}
}

func TestLinuxRegistrationRejectsUnitTraversalAndNewline(t *testing.T) {
	for _, o := range []LinuxRegistrationOptions{{Unit: "../escape.service", Directory: "/tmp", Executable: "/bin/true"}, {Unit: "test.service", Directory: "/tmp", Executable: "/bin/true\nExecStart=/bin/sh"}} {
		if _, err := NewLinuxRegistration(o).document(); err == nil {
			t.Fatal("拒绝不安全服务配置")
		}
	}
}
