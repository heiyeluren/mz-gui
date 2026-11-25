//go:build darwin || linux

package singleinstance

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func tryLock(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("获取单实例锁: %w", err)
	}
	return nil
}

func unlock(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("释放单实例锁: %w", err)
	}
	return nil
}
