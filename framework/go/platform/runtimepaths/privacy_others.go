//go:build !windows

package runtimepaths

import (
	"os"
	"syscall"
)

func isWindowsReparsePoint(os.FileInfo) bool { return false }

func lockdownPrivateDir(path string) error {
	return os.Chmod(path, 0o700)
}

func windowsOwnerOnly(string) (bool, bool) { return false, false }

func privateRegularFile(_ string, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}
