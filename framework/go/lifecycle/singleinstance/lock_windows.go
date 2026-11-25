//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func tryLock(file *os.File) error {
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrAlreadyRunning
	}
	if err != nil {
		return fmt.Errorf("获取单实例锁: %w", err)
	}
	return nil
}

func unlock(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return fmt.Errorf("释放单实例锁: %w", err)
	}
	return nil
}
