package executable

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserPATHPolicyDoesNotRelaxCompatibilityLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("需要创建符号链接的管理员或开发者模式权限")
	}
	root := t.TempDir()
	user, compat, target := filepath.Join(root, "user"), filepath.Join(root, "compat"), filepath.Join(root, "versions", "v9", "cli")
	for _, dir := range []string{user, compat, filepath.Dir(target)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{user, compat} {
		if err := os.Symlink(target, filepath.Join(dir, "cli")); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewResolver(Options{GOOS: "darwin", SearchPath: user, TrustedDirectories: []string{compat}, SearchPathFirst: true, AllowSearchPathSymlinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Resolve("cli"); err != nil || got != filepath.Join(user, "cli") {
		t.Fatalf("PATH 入口未生效: %q %v", got, err)
	}
	if err := os.Remove(filepath.Join(user, "cli")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("cli"); err == nil {
		t.Fatal("不能连带信任兼容目录的任意目标")
	}
}

func TestUserPATHPolicyKeepsFileAndPermissionValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX 可执行权限；Windows 由 PATHEXT 用例覆盖")
	}
	root := t.TempDir()
	target := filepath.Join(root, "file")
	if err := os.WriteFile(target, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(Options{GOOS: "darwin", SearchPath: ".:relative:" + root, SearchPathFirst: true, AllowSearchPathSymlinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("file"); err == nil {
		t.Fatal("非可执行文件不能当作 CLI")
	}
	if _, err := r.Resolve("../file"); err == nil {
		t.Fatal("命令名不能包含路径")
	}
	if r.SearchPath() != root {
		t.Fatalf("相对/空 PATH 必须排除: %q", r.SearchPath())
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("file"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("file"); err == nil {
		t.Fatal("权限撤销后不能复用结果")
	}
}
