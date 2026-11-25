// Package diskspace 查询目标路径所在文件系统对当前用户可用的空间。
package diskspace

import (
	"fmt"
	"os"
	"path/filepath"
)

// AvailableBytes 返回路径所在卷的可用字节数；路径尚未创建时查询最近的已存在父目录。
// 本函数只读，不创建目录，也不将其他读取错误误判为路径不存在。
func AvailableBytes(path string) (uint64, error) {
	if !filepath.IsAbs(path) {
		return 0, fmt.Errorf("磁盘空间查询需要绝对路径")
	}
	path = filepath.Clean(path)
	for {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return 0, fmt.Errorf("磁盘空间查询路径不是目录")
			}
			return availableBytes(path)
		}
		if !os.IsNotExist(err) {
			return 0, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}
