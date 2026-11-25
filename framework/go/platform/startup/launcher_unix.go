//go:build darwin || linux

package startup

import (
	"context"
	"os/exec"
	"syscall"
)

func startDetached(ctx context.Context, executable string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.Command(executable)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
