package testkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type FakePlatform struct {
	OS          string
	Arch        string
	Permissions map[string]bool
	Features    map[string]bool
}

func (platform FakePlatform) HasPermission(name string) bool {
	return platform.Permissions[name]
}

func (platform FakePlatform) HasFeature(name string) bool {
	return platform.Features[name]
}

func AssertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		want = 0o666
	}
	if info.Mode().Perm() != want {
		t.Fatalf("文件权限错误: path=%s mode=%o want=%o", path, info.Mode().Perm(), want)
	}
}

func RequireSymlink(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("probe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("当前 Windows 账户无符号链接权限，跳过该专项: %v", err)
	}
}
