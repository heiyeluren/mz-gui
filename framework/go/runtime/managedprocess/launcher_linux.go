//go:build linux

package managedprocess

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"os"
	"strconv"
	"strings"
)

type platformLauncher struct{}

const linuxManagedProcessSupervisorScript = `
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
	arguments := []string{"-c", linuxManagedProcessSupervisorScript, "mzgui-managed-process", spec.Executable}
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
	return &linuxProcess{command: command}, nil
}

type linuxProcess struct{ command *exec.Cmd }

func (process *linuxProcess) PID() int { return process.command.Process.Pid }
func (process *linuxProcess) Wait() (int, error) {
	err := process.command.Wait()
	if process.command.ProcessState == nil {
		return -1, err
	}
	return process.command.ProcessState.ExitCode(), err
}
func (process *linuxProcess) Interrupt() error {
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
func (process *linuxProcess) KillTree() error {
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

func (process *linuxProcess) WaitTree(ctx context.Context) error {
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

func (process *linuxProcess) groupExited() (bool, error) {
	// /proc 中仅查询本进程组，僵尸成员已退出，不等待其他父进程回收。
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		close := strings.LastIndex(string(data), ")")
		if close < 0 {
			continue
		}
		fields := strings.Fields(string(data)[close+1:])
		if len(fields) < 3 {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && group == process.PID() && fields[0] != "Z" && fields[0] != "X" {
			return false, nil
		}
	}
	return true, nil
}
