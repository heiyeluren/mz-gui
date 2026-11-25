//go:build darwin

package managedprocess

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinManagedProcessTerminatesWhenOwnerCrashes(t *testing.T) {
	if os.Getenv("MZGUI_MANAGED_PARENT_HELPER") == "1" {
		process, err := (platformLauncher{}).Launch(Spec{
			ID: "parent-death", Executable: os.Getenv("MZGUI_MANAGED_CHILD_SCRIPT"),
			Arguments: []string{os.Getenv("MZGUI_MANAGED_CHILD_PID_FILE")},
		})
		if err != nil {
			os.Exit(2)
		}
		_, _ = process.Wait()
		os.Exit(0)
	}

	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	script := filepath.Join(root, "child.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$$\" > \"$1\"\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(os.Args[0], "-test.run=^TestDarwinManagedProcessTerminatesWhenOwnerCrashes$")
	helper.Env = append(os.Environ(),
		"MZGUI_MANAGED_PARENT_HELPER=1",
		"MZGUI_MANAGED_CHILD_SCRIPT="+script,
		"MZGUI_MANAGED_CHILD_PID_FILE="+pidFile,
	)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := waitManagedChildPID(t, pidFile)
	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL)
		if helper.Process != nil {
			_ = helper.Process.Kill()
		}
	})
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("受管进程在 owner 崩溃后仍存活: pid=%d", childPID)
}

func waitManagedChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(body)))
			if parseErr == nil && pid > 1 {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("受管子进程未写入 PID")
	return 0
}
