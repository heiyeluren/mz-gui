//go:build darwin || linux

package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/creack/pty"
)

type darwinLauncher struct{ termProgram string }

func NewPlatformLauncher(options PlatformLauncherOptions) (Launcher, error) {
	termProgram, err := normalizeTermProgram(options)
	if err != nil {
		return nil, err
	}
	return darwinLauncher{termProgram: termProgram}, nil
}

func (launcher darwinLauncher) Launch(ctx context.Context, request LaunchRequest) (Device, Process, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if request.Multiplexer != nil {
		return launcher.launchMultiplexer(ctx, request)
	}
	var command *exec.Cmd
	if request.Executable != "" {
		command = exec.Command(request.Executable, request.Arguments...)
	} else {
		shell := "/bin/zsh"
		if runtime.GOOS == "linux" {
			shell = "/bin/bash"
		}
		if _, err := os.Stat(shell); err != nil {
			shell = "/bin/sh"
		}
		command = exec.Command(shell, "-l")
	}
	command.Dir = request.CWD
	command.Env = terminalProgramEnvironment(runtime.GOOS, allowedEnvironment(launcher.termProgram, "HOME", "USER", "LOGNAME", "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "SSH_AUTH_SOCK"), request.Environment)
	var file *os.File
	var err error
	size := &pty.Winsize{Cols: uint16(request.Cols), Rows: uint16(request.Rows)}
	if request.Stdin != nil {
		command.Stdin = bytes.NewReader(request.Stdin)
		// stdin是pipe时，仍由stdout上的PTY取得控制终端；不能把pipe fd0当成tty。
		file, err = pty.StartWithAttrs(command, size, &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1})
	} else {
		file, err = pty.StartWithSize(command, size)
	}
	if err != nil {
		return nil, nil, err
	}
	device := &darwinDevice{File: file}
	return device, &darwinProcess{command: command}, nil
}

type darwinDevice struct{ *os.File }

func (device *darwinDevice) Resize(cols, rows int) error {
	return pty.Setsize(device.File, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
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
	return err
}

func (process *darwinProcess) KillTree() error {
	err := syscall.Kill(-process.PID(), syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
