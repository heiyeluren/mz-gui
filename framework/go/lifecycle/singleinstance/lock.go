package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrAlreadyRunning = errors.New("应用实例已在运行")

type Options struct {
	Directory string
	FileName  string
}

// Lock 持有 agentd 生命周期内的跨进程排他锁。
type Lock struct {
	file *os.File
}

// Acquire 获取当前用户范围的进程锁。
func Acquire(options Options) (*Lock, error) {
	if options.Directory == "" {
		return nil, fmt.Errorf("单实例锁目录不能为空")
	}
	if err := validateFileName(options.FileName); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("创建单实例锁目录: %w", err)
	}
	if err := os.Chmod(options.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("设置单实例锁目录权限: %w", err)
	}
	path := filepath.Join(options.Directory, options.FileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开单实例锁: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("设置单实例锁权限: %w", err)
	}
	if err := tryLock(file); err != nil {
		file.Close()
		return nil, err
	}
	if err := recordPID(file); err != nil {
		_ = unlock(file)
		file.Close()
		return nil, err
	}
	return &Lock{file: file}, nil
}

func validateFileName(name string) error {
	if name == "" {
		return fmt.Errorf("单实例锁文件名不能为空")
	}
	if filepath.Base(name) != name || name == "." || strings.ContainsAny(name, "/\\\x00") {
		return fmt.Errorf("单实例锁文件名不安全: %q", name)
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("单实例锁文件名包含不支持的字符: %q", name)
	}
	return nil
}

func recordPID(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("清空单实例锁: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("定位单实例锁: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("记录单实例进程号: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步单实例锁: %w", err)
	}
	return nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := unlock(lock.file)
	closeErr := lock.file.Close()
	lock.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
