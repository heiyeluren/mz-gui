// Package filetrash 展示独立应用如何在用户确认后使用框架的回收站能力。
package filetrash

import (
	"context"
	"errors"
	frameworktrash "github.com/heiyeluren/mz-gui/framework/go/platform/filetrash"
)

func RecycleConfirmedDirectory(ctx context.Context, path string, confirmed bool) error {
	if !confirmed {
		return errors.New("请先确认要清理的目录")
	}
	return frameworktrash.Directory(ctx, path)
}
