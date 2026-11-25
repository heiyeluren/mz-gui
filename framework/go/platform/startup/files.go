package startup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func ValidateRegularFile(path string, requireExecutable bool) error {
	if !filepath.IsAbs(path) && !isWindowsAbsolute(path) {
		return errors.New("进程路径必须是绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("读取进程文件: %w", err)
	}
	if isLinkLike(info) || !info.Mode().IsRegular() {
		return errors.New("进程文件必须是普通文件且不能是符号链接")
	}
	if requireExecutable && !fileLooksExecutable(path, info.Mode()) {
		return errors.New("进程文件不可执行")
	}
	return nil
}

func WritePrivateAtomic(path string, data []byte) error {
	if !filepath.IsAbs(path) {
		return errors.New("启动注册文件路径必须是绝对路径")
	}
	if info, err := os.Lstat(path); err == nil && isLinkLike(info) {
		return errors.New("启动注册文件不能是符号链接")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".startup-registration-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := lockdownPrivateFile(temporaryPath); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("原子写入启动注册: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if isLinkLike(info) {
		_ = os.Remove(path)
		return errors.New("启动注册文件不能是符号链接")
	}
	if err := lockdownPrivateFile(path); err != nil {
		return err
	}
	return nil
}

func fileLooksExecutable(path string, mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(path), ".exe")
	}
	return mode.Perm()&0o111 != 0
}

func isLinkLike(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	return isWindowsReparsePoint(info)
}
