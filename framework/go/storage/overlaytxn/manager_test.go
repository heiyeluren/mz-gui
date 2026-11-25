package overlaytxn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerAppliesDeclaredFilesAndPreservesUnmanagedData(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(root, "rules", "common.md"), "old")
	writeFixture(t, filepath.Join(root, "config", "personal.toml"), "keep")
	writeFixture(t, filepath.Join(source, "rules", "common.md"), "new")
	writeFixture(t, filepath.Join(source, "bin", "mozi-tool"), "binary")
	manager, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Apply(context.Background(), ApplyRequest{TransactionID: "release-1", SourceRoot: source, Files: []File{
		{RelativePath: "rules/common.md"}, {RelativePath: "bin/mozi-tool", Executable: true},
	}})
	if err != nil || result.Applied != 2 || result.BackedUp != 1 {
		t.Fatalf("覆盖结果=%#v err=%v", result, err)
	}
	assertFixture(t, filepath.Join(root, "rules", "common.md"), "new")
	assertFixture(t, filepath.Join(root, "config", "personal.toml"), "keep")
	assertFixture(t, filepath.Join(result.BackupPath, "rules", "common.md"), "old")
	if info, statErr := os.Stat(filepath.Join(root, "bin", "mozi-tool")); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("可执行文件权限=%v err=%v", info, statErr)
	}
}

func TestManagerRejectsUnsafePathsAndLinks(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(source, "safe.md"), "safe")
	manager, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../escape", "/tmp/escape", "safe\\escape", "safe/../escape"} {
		if _, applyErr := manager.Apply(context.Background(), ApplyRequest{TransactionID: "unsafe", SourceRoot: source, Files: []File{{RelativePath: path}}}); !errors.Is(applyErr, ErrInvalidRequest) {
			t.Fatalf("路径 %q 应被拒绝，得到 %v", path, applyErr)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "rules")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(source, "rules", "common.md"), "new")
	if _, err := manager.Apply(context.Background(), ApplyRequest{TransactionID: "linked", SourceRoot: source, Files: []File{{RelativePath: "rules/common.md"}}}); !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("符号链接父目录应被拒绝，得到 %v", err)
	}
}

func TestManagerRollsBackEarlierFilesWhenLaterTargetFails(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(root, "rules", "common.md"), "old")
	writeFixture(t, filepath.Join(root, "skills"), "blocking-file")
	writeFixture(t, filepath.Join(source, "rules", "common.md"), "new")
	writeFixture(t, filepath.Join(source, "skills", "official", "tool.md"), "tool")
	manager, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Apply(context.Background(), ApplyRequest{TransactionID: "rollback", SourceRoot: source, Files: []File{
		{RelativePath: "rules/common.md"}, {RelativePath: "skills/official/tool.md"},
	}})
	if !errors.Is(err, ErrUnsafeTarget) {
		t.Fatalf("应返回不安全目标错误，得到 %v", err)
	}
	assertFixture(t, filepath.Join(root, "rules", "common.md"), "old")
}

func TestManagerHonorsCancellationBeforeMutation(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	writeFixture(t, filepath.Join(source, "rules", "common.md"), "new")
	manager, _ := New(Options{Root: root})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := manager.Apply(ctx, ApplyRequest{TransactionID: "cancelled", SourceRoot: source, Files: []File{{RelativePath: "rules/common.md"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "rules", "common.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("取消后不应写入目标，得到 %v", statErr)
	}
}

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFixture(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("读取 %s=%q err=%v", path, data, err)
	}
}
