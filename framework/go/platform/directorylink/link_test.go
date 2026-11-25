package directorylink

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryLinkLifecycleWithoutElevation(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "含 空格 & %", "source")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "entry")
	if err := Create(target, path); err != nil {
		t.Fatal(err)
	}
	if value, err := os.Readlink(path); err != nil || value != target {
		t.Fatalf("target=%q err=%v", value, err)
	}
	if info, err := os.Lstat(path); err != nil || !IsLink(info) {
		t.Fatalf("必须保留可识别的链接语义: %v %v", info, err)
	}
	if err := Create(target, path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("必须以已存在错误拒绝覆盖入口: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(path, "SKILL.md")); err != nil || string(body) != "updated" {
		t.Fatalf("入口没有同步目标修改: %s %v", body, err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if actual, err := Resolve(filepath.Join(path, "SKILL.md")); err != nil || actual != want {
		t.Fatalf("目标文件解析失败: %q %v", actual, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || string(body) != "updated" {
		t.Fatal("移除入口损坏目标")
	}
}

func TestRootedLinkRejectsParentJunctionEscape(t *testing.T) {
	base, outside, target := t.TempDir(), t.TempDir(), t.TempDir()
	if err := Create(outside, filepath.Join(base, "redirect")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := CreateInRoot(root, target, filepath.Join("redirect", "entry")); err == nil {
		t.Fatal("通过父目录链接逃逸了受控根")
	}
	if _, err := os.Lstat(filepath.Join(outside, "entry")); !os.IsNotExist(err) {
		t.Fatalf("根外出现副作用: %v", err)
	}
}

func TestDirectoryLinkCanRestoreTemporarilyMissingTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "temporarily-moved")
	path := filepath.Join(base, "entry")
	if err := Create(target, path); err != nil {
		t.Fatal(err)
	}
	if actual, err := os.Readlink(path); err != nil || actual != target {
		t.Fatalf("悬空入口无法恢复: %q %v", actual, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func TestRootedLinkRejectsEscapeAndPreservesExistingDirectory(t *testing.T) {
	base, target := t.TempDir(), t.TempDir()
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := CreateInRoot(root, target, "../escape"); err == nil {
		t.Fatal("允许入口逃逸")
	}
	if err := root.Mkdir("existing", 0700); err != nil {
		t.Fatal(err)
	}
	if err := CreateInRoot(root, target, "existing"); err == nil {
		t.Fatal("覆盖已有目录")
	}
	if info, err := root.Lstat("existing"); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("已有目录被修改")
	}
	if err := CreateInRoot(root, target, "entry"); err != nil {
		t.Fatal(err)
	}
	if actual, err := root.Readlink("entry"); err != nil || actual != target {
		t.Fatalf("根内入口读回失败: %q %v", actual, err)
	}
	if err := root.Remove("entry"); err != nil {
		t.Fatal(err)
	}
}
