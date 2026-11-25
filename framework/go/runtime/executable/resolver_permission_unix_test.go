//go:build !windows

package executable

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolverSkipsPermissionDeniedDirectoryAndContinues(t *testing.T) {
	blocked := t.TempDir()
	available := t.TempDir()
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })

	executable := filepath.Join(available, "codex")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{
		GOOS:               "darwin",
		TrustedDirectories: []string{blocked, available},
	})
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := resolver.Resolve("codex")
	if err != nil || resolved != executable {
		t.Fatalf("无权限目录后仍应继续解析: path=%q err=%v", resolved, err)
	}
}

func TestResolverRejectsSymlinkTargetOutsideTrustedDirectories(t *testing.T) {
	trusted := t.TempDir()
	untrusted := t.TempDir()
	target := filepath.Join(untrusted, "codex")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(trusted, "codex")); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{trusted}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("codex"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("指向非受信目录的 symlink 必须拒绝: %v", err)
	}
}

func TestResolverAcceptsSymlinkTargetInsideAnotherTrustedDirectory(t *testing.T) {
	linked := t.TempDir()
	targetRoot := t.TempDir()
	target := filepath.Join(targetRoot, "codex")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linked, "codex")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{linked, targetRoot}})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve("codex")
	if err != nil || resolved != link {
		t.Fatalf("指向另一受信目录的 symlink 应保留链接入口: path=%q err=%v", resolved, err)
	}
}

func TestResolverAcceptsSymlinkTargetInsideTrustedTargetDirectoryWithoutSearchingIt(t *testing.T) {
	linked := t.TempDir()
	targetRoot := t.TempDir()
	target := filepath.Join(targetRoot, "codex")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linked, "codex")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(Options{
		GOOS:                     "darwin",
		TrustedDirectories:       []string{linked},
		TrustedTargetDirectories: []string{targetRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve("codex")
	if err != nil || resolved != link {
		t.Fatalf("受信目标目录中的 symlink 应保留入口: path=%q err=%v", resolved, err)
	}
	if resolver.SearchPath() != linked {
		t.Fatalf("目标目录不得进入执行 PATH: %q", resolver.SearchPath())
	}
}
