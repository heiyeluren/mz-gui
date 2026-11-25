//go:build !windows

package startup

import "os"

func isWindowsReparsePoint(os.FileInfo) bool { return false }

func lockdownPrivateFile(path string) error {
	return os.Chmod(path, 0o600)
}

func windowsOwnerOnly(string) (bool, bool) { return false, false }
