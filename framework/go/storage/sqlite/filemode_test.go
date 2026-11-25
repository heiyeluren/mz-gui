package sqlite

import (
	"os"
	"runtime"
	"testing"
)

func TestIsRestrictedFileModeAcceptsUnixOwnerOnlyAndWindowsRegularFiles(t *testing.T) {
	if runtime.GOOS != "windows" && !IsRestrictedFileMode(os.FileMode(0o600)) {
		t.Fatal("Unix 上 0600 必须视为受限")
	}
	if runtime.GOOS != "windows" && IsRestrictedFileMode(os.FileMode(0o644)) {
		t.Fatal("Unix 上 0644 不得视为受限")
	}
	if runtime.GOOS == "windows" && !IsRestrictedFileMode(os.FileMode(0o666)) {
		t.Fatal("Windows 上普通文件必须视为可接受的受限结果")
	}
	if IsRestrictedFileMode(os.ModeDir | 0o700) {
		t.Fatal("目录不得视为受限数据库文件")
	}
}
