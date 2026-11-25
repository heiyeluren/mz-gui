package startup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateRegularFileAndWritePrivateAtomic(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "sample-worker")
	if err := os.WriteFile(plain, []byte("worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegularFile(plain, true); err == nil {
		t.Fatal("缺少执行权限必须拒绝")
	}
	if runtime.GOOS == "windows" {
		executable := filepath.Join(root, "sample-worker.exe")
		if err := os.WriteFile(executable, []byte("worker"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateRegularFile(executable, true); err != nil {
			t.Fatalf("Windows .exe 必须视为可执行: %v", err)
		}
	}
	if err := ValidateRegularFile(plain, false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegularFile("relative", false); err == nil {
		t.Fatal("相对路径必须拒绝")
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegularFile(directory, false); err == nil {
		t.Fatal("目录必须拒绝")
	}
	target := filepath.Join(root, "registration.plist")
	if err := WritePrivateAtomic(target, []byte("v1")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "v1" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	assertPrivateFile(t, target)
	link := filepath.Join(root, "registration-link.plist")
	createFileLinkLike(t, target, link)
	if err := WritePrivateAtomic(link, []byte("v2")); err == nil {
		t.Fatal("符号链接目标必须拒绝")
	}
	if err := WritePrivateAtomic("relative", []byte("v2")); err == nil {
		t.Fatal("相对注册路径必须拒绝")
	}
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("读取文件 %s: %v", path, err)
	}
	if isLinkLike(info) || !info.Mode().IsRegular() {
		t.Fatalf("必须是普通文件: %s mode=%v", path, info.Mode())
	}
	if runtime.GOOS == "windows" {
		if ownerOnly, queryable := windowsOwnerOnly(path); queryable && !ownerOnly {
			t.Fatalf("Windows 文件 ACL 不是 owner-only: %s", path)
		}
		return
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode().Perm())
	}
}
