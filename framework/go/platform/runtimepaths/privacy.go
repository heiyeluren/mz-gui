package runtimepaths

import (
	"fmt"
	"os"
)

func isLinkLike(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return isWindowsReparsePoint(info)
}

// VerifyPrivateRegularFile 校验文件没有经过链接跳转，并且仅受当前用户控制。
func VerifyPrivateRegularFile(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("私有文件路径不能为空")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || isLinkLike(info) {
		return nil, fmt.Errorf("私有文件必须是普通文件: %s", path)
	}
	if !privateRegularFile(path, info) {
		return nil, fmt.Errorf("私有文件所有者或权限不安全: %s", path)
	}
	return info, nil
}
