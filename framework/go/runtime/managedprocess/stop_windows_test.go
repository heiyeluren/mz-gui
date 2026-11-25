//go:build windows

package managedprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsStopJobContract(t *testing.T) {
	if unsafe.Sizeof(jobAccountingInformation{}) != 48 || unsafe.Offsetof(jobAccountingInformation{}.ActiveProcesses) != 40 {
		t.Fatal("Job accounting ABI 不匹配")
	}
	p := &windowsProcess{job: windows.InvalidHandle}
	if err := p.KillTree(); err == nil {
		t.Fatal("Job 终止错误不能被吞掉")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.WaitTree(ctx); err == nil {
		t.Fatal("Job 查询失败不得视为退出确认")
	}
}

func TestWindowsStopConfirmsJobChildrenExited(t *testing.T) {
	root := t.TempDir()
	manager := New()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := manager.Start(ctx, Spec{ID: "job-tree-fixture", Executable: testExecutablePath(),
		Arguments:   []string{"-test.run=^TestWindowsManagedTreeHelper$"},
		Environment: map[string]string{"MZGUI_TREE_HELPER": "parent", "MZGUI_TREE_ROOT": root},
		Restart:     RestartConfig{Policy: RestartNever}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Stop(ctx)
	})
	var child windows.Handle
	for ctx.Err() == nil {
		body, err := os.ReadFile(filepath.Join(root, "child.pid"))
		if err == nil {
			pid, parseErr := strconv.ParseUint(strings.TrimSpace(string(body)), 10, 32)
			if parseErr == nil && pid > 1 {
				child, err = windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
				if err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("未收到隔离子进程 PID")
	}
	t.Cleanup(func() { _ = windows.TerminateProcess(child, 1); _ = windows.CloseHandle(child) })
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := windows.WaitForSingleObject(child, 0)
	if err != nil || state != windows.WAIT_OBJECT_0 || manager.Status().Running {
		t.Fatalf("Stop 成功后子进程必须已退出: state=%d err=%v", state, err)
	}
}

func TestWindowsManagedTreeHelper(t *testing.T) {
	mode := os.Getenv("MZGUI_TREE_HELPER")
	if mode == "" {
		return
	}
	if mode == "parent" {
		child := exec.Command(os.Args[0], "-test.run=^TestWindowsManagedTreeHelper$")
		child.Env = append(os.Environ(), "MZGUI_TREE_HELPER=child")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("MZGUI_TREE_ROOT"), "child.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			_ = child.Process.Kill()
			t.Fatal(err)
		}
		if err := child.Wait(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return
		}
		return
	}
	time.Sleep(30 * time.Second)
}
