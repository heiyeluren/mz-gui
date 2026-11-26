package singleinstance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireRejectsConcurrentInstanceAndAllowsRestart(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	options := Options{Directory: runDir, FileName: "application.lock"}
	first, err := Acquire(options)
	if err != nil {
		t.Fatalf("获取首个锁: %v", err)
	}
	second, err := Acquire(options)
	if second != nil || !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("重复实例未被拒绝: lock=%v err=%v", second, err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("释放首个锁: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(runDir, "application.lock"))
	if err != nil || strings.TrimSpace(string(data)) == "" {
		t.Fatalf("锁文件未记录进程号: data=%q err=%v", data, err)
	}

	third, err := Acquire(options)
	if err != nil {
		t.Fatalf("进程退出后无法重新获取锁: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("重复关闭必须幂等: %v", err)
	}
}

func TestAcquireRejectsEmptyRunDirectory(t *testing.T) {
	if _, err := Acquire(Options{FileName: "application.lock"}); err == nil {
		t.Fatal("空锁目录必须拒绝")
	}
}

func TestAcquireRejectsUnsafeFileName(t *testing.T) {
	runDir := t.TempDir()
	for _, name := range []string{"", "../escape.lock", "nested/application.lock", `nested\application.lock`, "应用.lock"} {
		if _, err := Acquire(Options{Directory: runDir, FileName: name}); err == nil {
			t.Errorf("不安全的锁文件名必须拒绝: %q", name)
		}
	}
}
