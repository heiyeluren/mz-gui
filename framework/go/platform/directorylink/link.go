// Package directorylink 提供普通用户可创建、可读回且不复制目标内容的目录入口。
package directorylink

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Create 创建目录链接，拒绝覆盖任何已有对象。Windows 使用不要求符号链接权限的 junction。
func Create(target, path string) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return CreateInRoot(root, target, filepath.Base(path))
}

// CreateInRoot 将新入口限制在 root 内；目标可以在 root 外，由调用方验证目标归属。
func CreateInRoot(root *os.Root, target, name string) error {
	if root == nil || !filepath.IsAbs(target) || !filepath.IsLocal(name) || name == "." {
		return fmt.Errorf("目录链接必须使用绝对目标和根内相对入口")
	}
	info, err := os.Stat(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !info.IsDir() {
		return fmt.Errorf("目录链接目标不是目录")
	}
	return create(root, filepath.Clean(target), name)
}

// IsLink 统一识别符号链接与 Windows reparse 入口；调用方仍需核验实际目标。
func IsLink(info os.FileInfo) bool {
	return info != nil && (info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info))
}
