//go:build !windows

package xterm

import "os/exec"

func configureStateProcess(*exec.Cmd) {}
