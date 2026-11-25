//go:build windows

package startup

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func startDetached(ctx context.Context, executable string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.Command(executable)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		HideWindow:    true,
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
