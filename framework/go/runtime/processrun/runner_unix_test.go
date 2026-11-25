//go:build darwin || linux

package processrun

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCleansDescendantsAfterRootExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := t.TempDir() + "/child.pid"
	result := Run(context.Background(), Spec{
		Executable: executable,
		Arguments:  []string{"-test.run=^TestProcessRunHelper$"},
		Environment: append(os.Environ(),
			"MZ_GUI_PROCESSRUN_HELPER=spawn-child", "MZ_GUI_PROCESSRUN_PID_FILE="+pidFile),
	})
	if result.Err != nil || result.ExitCode != 0 {
		t.Fatalf("父进程执行结果=%#v", result)
	}
	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("后代进程 %d 未被清理: %v", pid, err)
}
