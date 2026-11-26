package blobquota

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
	"time"
)

func TestPruneRemovesExpiredAndOldestUntilUnderQuota(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 16, 21, 0, 0, 0, time.UTC)
	old := filepath.Join(root, "old.bin")
	keep := filepath.Join(root, "keep.bin")
	if err := os.WriteFile(old, []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("bbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := now.Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keep, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	result, err := Prune(root, 8, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.Kept != 1 || result.Bytes != 4 {
		t.Fatalf("过期文件应被清理: %#v", result)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("过期文件必须删除")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("未过期文件必须保留")
	}

	heavy := filepath.Join(root, "heavy.bin")
	if err := os.WriteFile(heavy, []byte("cccccccc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keep, now.Add(-10*time.Minute), now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(heavy, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	result, err = Prune(root, 4, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || result.Bytes != 4 {
		t.Fatalf("超配额应删最旧文件: %#v", result)
	}
	if _, err := os.Stat(heavy); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("最旧大文件必须删除")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("较新小文件必须保留")
	}
}

func TestPruneRejectsUnsafeRootAndSkipsSymlinks(t *testing.T) {
	testkit.RequireSymlink(t)
	now := time.Now().UTC()
	if _, err := Prune("relative", 8, 0, now); !errors.Is(err, ErrInvalidQuota) {
		t.Fatalf("相对路径必须拒绝: %v", err)
	}
	if _, err := Prune("/tmp", 0, 0, now); !errors.Is(err, ErrInvalidQuota) {
		t.Fatalf("非法配额必须拒绝: %v", err)
	}

	root := t.TempDir()
	target := filepath.Join(root, "real.bin")
	if err := os.WriteFile(target, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias.bin")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	result, err := Prune(root, 1024, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kept != 1 || result.Removed != 0 {
		t.Fatalf("符号链接不得计入或删除: %#v", result)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("符号链接必须跳过")
	}
}
