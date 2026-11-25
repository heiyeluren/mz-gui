//go:build darwin || linux

package processrun

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func launch(ctx context.Context, spec Spec) (process, error) {
	command := exec.Command(spec.Executable, spec.Arguments...)
	if spec.stdin != nil {
		args := append([]string{"-c", streamSupervisorScript, "mzgui-stream-process", spec.Executable}, spec.Arguments...)
		command = exec.Command("/bin/sh", args...)
	}
	command.Dir = spec.Directory
	command.Env = append([]string(nil), spec.Environment...)
	command.Stdin = bytes.NewReader(spec.Input)
	if spec.stdin != nil {
		command.Stdin = spec.stdin
	}
	command.Stdout = spec.stdout
	command.Stderr = spec.stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &darwinProcess{command: command}, nil
}

// 持续通信进程不能依赖协议对端收到 EOF 后自行退出，宿主崩溃也回收本进程组。
const streamSupervisorScript = `
parent="$PPID"
(
  while [ "$(/bin/ps -o ppid= -p "$$" 2>/dev/null | /usr/bin/tr -d ' ')" = "$parent" ]; do
    sleep 1
  done
  /bin/kill -KILL -- "-$$" 2>/dev/null || true
) &
watcher="$!"
"$@"
status="$?"
kill "$watcher" 2>/dev/null || true
wait "$watcher" 2>/dev/null || true
exit "$status"
`

type darwinProcess struct {
	command *exec.Cmd
}

func (process *darwinProcess) PID() int { return process.command.Process.Pid }

func (process *darwinProcess) Wait() (int, error) {
	err := process.command.Wait()
	if process.command.ProcessState == nil {
		return -1, err
	}
	return process.command.ProcessState.ExitCode(), err
}

func (process *darwinProcess) KillTree() error {
	err := syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (process *darwinProcess) Kill() error {
	err := process.command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (*darwinProcess) Close() error { return nil }
