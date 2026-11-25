//go:build darwin || linux

package processrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStreamParentCrashHelper(t *testing.T) {
	if os.Getenv("MZ_STREAM_PARENT") != "1" {
		return
	}
	s, err := StartStream(context.Background(), StreamSpec{Executable: "/bin/sh", Arguments: []string{"-c", "while :; do sleep 1; done"}, Environment: os.Environ()})
	if err != nil {
		os.Exit(2)
	}
	fmt.Println(s.PID())
	time.Sleep(100 * time.Millisecond)
	os.Exit(0)
}

func TestStreamParentCrashStopsOwnedProcessGroup(t *testing.T) {
	p := exec.Command(os.Args[0], "-test.run=^TestStreamParentCrashHelper$")
	p.Env = append(os.Environ(), "MZ_STREAM_PARENT=1")
	output, err := p.Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("宿主退出后持续通信子进程仍在运行")
}
