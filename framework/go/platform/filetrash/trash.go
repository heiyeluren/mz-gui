// Package filetrash 提供可恢复的系统回收站操作；失败时不回退到永久删除。
package filetrash

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

var ErrUnavailable = errors.New("系统回收站操作未确认成功，请检查原目录")

// Directory 将普通目录移入系统回收站并回验原路径已消失。
// 调用方负责确认用户授权及目录业务归属。
func Directory(ctx context.Context, path string) error {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return errors.New("回收目录必须是安全的绝对路径")
	}
	home, err := os.UserHomeDir()
	if err != nil || path == filepath.Clean(home) {
		return errors.New("不能回收用户主目录")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("回收目标必须是无符号链接的真实目录")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = moveDirectory(ctx, path); err != nil {
		return err
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("回收后原路径仍存在或无法确认，停止后续操作")
	}
	return nil
}
