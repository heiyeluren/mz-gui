//go:build linux

package filetrash

import (
	"context"
	"os/exec"
)

func moveDirectory(ctx context.Context, path string) error {
	tool, err := exec.LookPath("gio")
	if err != nil {
		return ErrUnavailable
	}
	if err = exec.CommandContext(ctx, tool, "trash", "--", path).Run(); err != nil {
		return ErrUnavailable
	}
	return nil
}
