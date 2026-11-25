//go:build !windows

package directorylink

import (
	"os"
	"path/filepath"
)

func create(root *os.Root, target, name string) error { return root.Symlink(target, name) }

func isReparsePoint(os.FileInfo) bool { return false }

// Resolve 返回链接指向的真实路径，调用方必须再验证该路径是否受信任。
func Resolve(path string) (string, error) { return filepath.EvalSymlinks(path) }
