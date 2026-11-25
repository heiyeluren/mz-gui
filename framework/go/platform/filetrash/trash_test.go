package filetrash

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirectoryRejectsUnsafePathsBeforeNativeOperation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err = os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err = os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	for _, path := range []string{"relative", filepath.VolumeName(root) + string(os.PathSeparator), home, file, filepath.Join(root, "missing")} {
		if err = Directory(context.Background(), path); err == nil {
			t.Fatalf("接受不安全路径 %s", path)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = Directory(ctx, child); err == nil {
		t.Fatal("取消后仍然清理")
	}
	if _, err = os.Stat(child); err != nil {
		t.Fatal("拒绝时删除了目录")
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err = os.Symlink(child, link); err != nil {
			t.Fatal(err)
		}
		if err = Directory(context.Background(), link); err == nil {
			t.Fatal("允许符号链接")
		}
		if _, err = os.Stat(child); err != nil {
			t.Fatal("链接目标丢失")
		}
	}
}

func TestDirectoryNativeRecycle(t *testing.T) {
	if os.Getenv("MZGUI_TEST_TRASH_NATIVE") != "1" {
		t.Skip("仅显式原生验收时使用本测试新建的目录")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "mzgui-trash-"+filepath.Base(root))
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "recovery.txt"), []byte("only-test-created-files"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Directory(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("原路径未清空: %v", err)
	}
	t.Log("本测试目录已进入系统回收站：", filepath.Base(path))
}
