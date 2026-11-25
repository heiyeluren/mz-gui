package remotedebug

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
)

func RunCommand(ctx context.Context, command string) Result {
	command = strings.TrimSpace(command)
	if command == "" {
		return Result{ExitCode: 2, Stderr: "命令不能为空"}
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exit.ExitCode()
		} else {
			result.ExitCode = 1
			if result.Stderr == "" {
				result.Stderr = err.Error()
			}
		}
		return result
	}
	return result
}
