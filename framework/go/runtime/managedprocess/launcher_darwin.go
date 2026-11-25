//go:build darwin

package managedprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type platformLauncher struct{}

const darwinManagedProcessSupervisorScript = `
parent="$PPID"
"$@" &
managed="$!"
(
  while [ "$(/bin/ps -o ppid= -p "$$" 2>/dev/null | /usr/bin/tr -d ' ')" = "$parent" ]; do
    sleep 0.1
  done
  /bin/kill -KILL -- "-$$" 2>/dev/null || true
) &
watcher="$!"
wait "$managed"
status="$?"
kill "$watcher" 2>/dev/null || true
wait "$watcher" 2>/dev/null || true
exit "$status"
`

func (platformLauncher) Launch(spec Spec) (process, error) {
	arguments := []string{"-c", darwinManagedProcessSupervisorScript, "mzgui-managed-process", spec.Executable}
	arguments = append(arguments, spec.Arguments...)
	command := exec.Command("/bin/sh", arguments...)
	command.Dir = spec.Directory
	command.Env = mergedEnvironment(spec.Environment)
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &darwinProcess{command: command}, nil
}

type darwinProcess struct{ command *exec.Cmd }

func (process *darwinProcess) PID() int { return process.command.Process.Pid }
func (process *darwinProcess) Wait() (int, error) {
	err := process.command.Wait()
	if process.command.ProcessState == nil {
		return -1, err
	}
	return process.command.ProcessState.ExitCode(), err
}
func (process *darwinProcess) Interrupt() error {
	err := syscall.Kill(-process.PID(), syscall.SIGINT)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if errors.Is(err, syscall.EPERM) {
		if exited, queryErr := process.groupExited(); queryErr == nil && exited {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("中断进程组 %d: %w", process.PID(), err)
	}
	return nil
}
func (process *darwinProcess) KillTree() error {
	err := syscall.Kill(-process.PID(), syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if errors.Is(err, syscall.EPERM) {
		if exited, queryErr := process.groupExited(); queryErr == nil && exited {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("终止进程组 %d: %w", process.PID(), err)
	}
	return nil
}

func (process *darwinProcess) WaitTree(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		exited, err := process.groupExited()
		if err != nil {
			return fmt.Errorf("查询进程组 %d: %w", process.PID(), err)
		}
		if exited {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrTerminationTimeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (process *darwinProcess) groupExited() (bool, error) {
	// 定向查询自己的进程组；信号 0 在 macOS 的退出竞争窗口可能返回 EPERM。
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", process.PID())
	return len(members) == 0, err
}
