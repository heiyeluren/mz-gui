package directorytxn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPermanentRemoveDeletesOnlySelectedDirectory(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.TargetPath("demo"), "remove")
	writePreparedTree(t, manager.TargetPath("keep"), "keep")
	writePreparedTree(t, manager.BackupPath("old"), "old backup")
	result, err := manager.Remove(context.Background(), RemoveRequest{Key: "demo", BackupID: "remove", Permanent: true})
	if err != nil || result.BackupPath != "" || !result.HadPrevious {
		t.Fatalf("永久删除结果错误: %#v %v", result, err)
	}
	for _, path := range []string{manager.TargetPath("demo"), manager.BackupPath("remove")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("永久删除仍留下目录 %s: %v", path, err)
		}
	}
	assertTreeContent(t, manager.TargetPath("keep"), "keep")
	assertTreeContent(t, manager.BackupPath("old"), "old backup")
}

func TestPermanentRemoveRejectsUnsafePathsAndCancellation(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.TargetPath("demo"), "keep")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Remove(ctx, RemoveRequest{Key: "demo", BackupID: "remove", Permanent: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消删除未被拒绝: %v", err)
	}
	if _, err := manager.Remove(context.Background(), RemoveRequest{Key: "../demo", BackupID: "remove", Permanent: true}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("越界删除未被拒绝: %v", err)
	}
	outside := t.TempDir()
	writePreparedTree(t, filepath.Join(outside, "external"), "external")
	if err := os.Symlink(outside, filepath.Join(manager.TargetPath("demo"), "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Remove(context.Background(), RemoveRequest{Key: "demo", BackupID: "remove", Permanent: true}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("含软链的删除未被拒绝: %v", err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "keep")
	assertTreeContent(t, filepath.Join(outside, "external"), "external")
}

func TestRestoreRemovedDirectoryRejectsExistingTarget(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.BackupPath("old"), "old content")
	writePreparedTree(t, manager.TargetPath("demo"), "new content")
	_, err := manager.RollbackReplace(context.Background(), RollbackReplaceRequest{Key: "demo", BackupID: "old", FailedID: "restore", HadPrevious: true, RequireTargetAbsent: true})
	if !errors.Is(err, ErrRollbackConflict) {
		t.Fatalf("应拒绝覆盖新目录: %v", err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "new content")
	assertTreeContent(t, manager.BackupPath("old"), "old content")
}

func TestManagerInstallsReplacesAndRemovesDirectory(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.StagingPath("install-1"), "version one")

	if manager.TargetRoot() == "" || filepath.Base(manager.TargetRoot()) != "installed" && !filepath.IsAbs(manager.TargetRoot()) {
		t.Fatalf("目标根目录无效: %q", manager.TargetRoot())
	}
	installed, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "install-1", BackupID: "backup-1"})
	if err != nil || installed.HadPrevious || installed.BackupPath != "" {
		t.Fatalf("首次安装结果错误: %#v err=%v", installed, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "version one")

	writePreparedTree(t, manager.StagingPath("install-2"), "version two")
	replaced, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "install-2", BackupID: "backup-2"})
	if err != nil || !replaced.HadPrevious || replaced.BackupPath != manager.BackupPath("backup-2") {
		t.Fatalf("替换安装结果错误: %#v err=%v", replaced, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "version two")
	assertTreeContent(t, manager.BackupPath("backup-2"), "version one")

	removed, err := manager.Remove(context.Background(), RemoveRequest{Key: "demo", BackupID: "backup-3"})
	if err != nil || removed.BackupPath != manager.BackupPath("backup-3") {
		t.Fatalf("卸载备份结果错误: %#v err=%v", removed, err)
	}
	if _, err := os.Lstat(manager.TargetPath("demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("卸载后目标目录仍存在: %v", err)
	}
	assertTreeContent(t, manager.BackupPath("backup-3"), "version two")
}

func TestManagerPreservesCurrentTargetOnValidationAndConflict(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.TargetPath("demo"), "current")

	if _, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "missing", BackupID: "backup-missing"}); !errors.Is(err, ErrStagingMissing) {
		t.Fatalf("缺失暂存目录错误不稳定: %v", err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "current")

	writePreparedTree(t, manager.StagingPath("install-conflict"), "candidate")
	writePreparedTree(t, manager.BackupPath("backup-conflict"), "occupied")
	if _, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "install-conflict", BackupID: "backup-conflict"}); !errors.Is(err, ErrBackupConflict) {
		t.Fatalf("备份冲突错误不稳定: %v", err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "current")
	assertTreeContent(t, manager.StagingPath("install-conflict"), "candidate")
}

func TestManagerRecoversReplacementInterruptedAfterBackup(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.StagingPath("recover-install"), "candidate")
	writePreparedTree(t, manager.BackupPath("recover-backup"), "previous")

	result, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "recover-install", BackupID: "recover-backup"})
	if err != nil || !result.HadPrevious || result.BackupPath != manager.BackupPath("recover-backup") {
		t.Fatalf("中断替换恢复失败: %#v err=%v", result, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "candidate")
	assertTreeContent(t, manager.BackupPath("recover-backup"), "previous")
}

func TestManagerRejectsUnsafeNamesRootsAndTrees(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", ".", "../escape", "nested/value", `nested\value`, "含中文"} {
		if validComponent(name) {
			t.Fatalf("不安全目录分量被接受: %q", name)
		}
	}
	if _, err := New(Options{Root: "relative"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("相对根目录错误不稳定: %v", err)
	}

	realRoot := filepath.Join(root, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(root, "linked")
	if err := os.Symlink(realRoot, linkedRoot); err == nil {
		if _, err := New(Options{Root: linkedRoot}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("软链根目录错误不稳定: %v", err)
		}
	}

	manager := newTestManager(t)
	staging := manager.StagingPath("unsafe-tree")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(staging, "link")); err == nil {
		if _, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "unsafe-tree", BackupID: "backup"}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("包含软链的暂存目录错误不稳定: %v", err)
		}
	}
}

func TestManagerHonoursContextAndMissingTarget(t *testing.T) {
	manager := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writePreparedTree(t, manager.StagingPath("cancelled"), "candidate")
	if _, err := manager.Replace(ctx, ReplaceRequest{Key: "demo", StagingID: "cancelled", BackupID: "backup"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误未保留: %v", err)
	}
	if _, err := manager.Remove(context.Background(), RemoveRequest{Key: "missing", BackupID: "backup"}); !errors.Is(err, ErrTargetMissing) {
		t.Fatalf("缺失卸载目标错误不稳定: %v", err)
	}
}

func TestManagerRollsBackReplacementAndFirstInstall(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.TargetPath("demo"), "previous")
	writePreparedTree(t, manager.StagingPath("install-update"), "candidate")
	replaced, err := manager.Replace(context.Background(), ReplaceRequest{Key: "demo", StagingID: "install-update", BackupID: "backup-update"})
	if err != nil || !replaced.HadPrevious {
		t.Fatalf("准备替换失败: %#v err=%v", replaced, err)
	}
	rolledBack, err := manager.RollbackReplace(context.Background(), RollbackReplaceRequest{
		Key: "demo", BackupID: "backup-update", FailedID: "failed-update", HadPrevious: true,
	})
	if err != nil || !rolledBack.RestoredPrevious {
		t.Fatalf("升级回滚失败: %#v err=%v", rolledBack, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "previous")
	if _, err := os.Lstat(manager.BackupPath("backup-update")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("回滚后备份未消费: %v", err)
	}
	if _, err := os.Lstat(manager.StagingPath("failed-update")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("回滚后失败目录未清理: %v", err)
	}

	writePreparedTree(t, manager.StagingPath("install-new"), "new")
	installed, err := manager.Replace(context.Background(), ReplaceRequest{Key: "new-skill", StagingID: "install-new", BackupID: "backup-new"})
	if err != nil || installed.HadPrevious {
		t.Fatalf("准备首次安装失败: %#v err=%v", installed, err)
	}
	if _, err := manager.RollbackReplace(context.Background(), RollbackReplaceRequest{
		Key: "new-skill", BackupID: "backup-new", FailedID: "failed-new", HadPrevious: false,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(manager.TargetPath("new-skill")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("首次安装回滚后目标仍存在: %v", err)
	}
}

func TestManagerRollbackReplacementRecoversInterruptedRestore(t *testing.T) {
	manager := newTestManager(t)
	writePreparedTree(t, manager.BackupPath("backup-update"), "previous")
	writePreparedTree(t, manager.StagingPath("failed-update"), "candidate")
	result, err := manager.RollbackReplace(context.Background(), RollbackReplaceRequest{
		Key: "demo", BackupID: "backup-update", FailedID: "failed-update", HadPrevious: true,
	})
	if err != nil || !result.RestoredPrevious {
		t.Fatalf("中断回滚恢复失败: %#v err=%v", result, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "previous")
	if _, err := manager.RollbackReplace(context.Background(), RollbackReplaceRequest{
		Key: "demo", BackupID: "backup-update", FailedID: "failed-update", HadPrevious: true,
	}); err != nil {
		t.Fatalf("回滚幂等重放失败: %v", err)
	}

	manager = newTestManager(t)
	writePreparedTree(t, manager.TargetPath("demo"), "previous")
	writePreparedTree(t, manager.StagingPath("failed-update"), "candidate")
	result, err = manager.RollbackReplace(context.Background(), RollbackReplaceRequest{
		Key: "demo", BackupID: "backup-update", FailedID: "failed-update", HadPrevious: true,
	})
	if err != nil || !result.RestoredPrevious {
		t.Fatalf("恢复完成后的隔离目录清理失败: %#v err=%v", result, err)
	}
	assertTreeContent(t, manager.TargetPath("demo"), "previous")
	if _, err := os.Lstat(manager.StagingPath("failed-update")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("中断回滚的隔离目录未清理: %v", err)
	}
}

func TestManagerPrunesOnlyExpiredSafeBackups(t *testing.T) {
	manager := newTestManager(t)
	oldPath := manager.BackupPath("old-backup")
	newPath := manager.BackupPath("new-backup")
	writePreparedTree(t, oldPath, "old")
	writePreparedTree(t, newPath, "new")
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	result, err := manager.PruneBackups(context.Background(), now.Add(-24*time.Hour))
	if err != nil || len(result.Removed) != 1 || result.Removed[0] != "old-backup" {
		t.Fatalf("备份保留清理结果错误: %#v err=%v", result, err)
	}
	if _, err := os.Lstat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("过期备份未清理: %v", err)
	}
	assertTreeContent(t, newPath, "new")

	if _, err := manager.PruneBackups(context.Background(), time.Time{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("空截止时间错误不稳定: %v", err)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := New(Options{Root: filepath.Join(t.TempDir(), "managed")})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func writePreparedTree(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "value.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTreeContent(t *testing.T, root, expected string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "nested", "value.txt"))
	if err != nil || string(data) != expected {
		t.Fatalf("目录内容错误: value=%q err=%v", data, err)
	}
}
