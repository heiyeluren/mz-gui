//go:build windows

package xterm

import (
	"os/exec"
	"syscall"
)

func configureStateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
