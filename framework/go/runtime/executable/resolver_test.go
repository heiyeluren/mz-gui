package executable

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolverFindsExecutableInTrustedDirectoryOutsideProcessPath(t *testing.T) {
	trusted := t.TempDir()
	path := filepath.Join(trusted, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("目标路径判断: /bin=%v trusted=%v", isAbsPathForGOOS("darwin", "/bin"), isAbsPathForGOOS("darwin", trusted))
	resolver, err := NewResolver(Options{
		GOOS:               "darwin",
		SearchPath:         "/usr/bin:/bin:/usr/sbin:/sbin",
		TrustedDirectories: []string{trusted},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve("codex")
	if err != nil || resolved != path {
		t.Fatalf("受信目录解析失败: path=%q err=%v", resolved, err)
	}
}

func TestResolverRejectsPathsAndNonExecutableFiles(t *testing.T) {
	trusted := t.TempDir()
	if err := os.WriteFile(filepath.Join(trusted, "codex"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{trusted}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("../codex"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("路径逃逸错误=%v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := resolver.Resolve("codex"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("非可执行文件错误=%v", err)
		}
	}
}

func TestResolverAcceptsWindowsPATHEXTInTrustedDirectory(t *testing.T) {
	trusted := t.TempDir()
	path := filepath.Join(trusted, "codex.CMD")
	if err := os.WriteFile(path, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{
		GOOS: "windows", PATHEXT: ".COM;.EXE;.BAT;.CMD", TrustedDirectories: []string{trusted},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve("codex")
	if err != nil || resolved != path {
		t.Fatalf("Windows PATHEXT 解析失败: path=%q err=%v", resolved, err)
	}
}
