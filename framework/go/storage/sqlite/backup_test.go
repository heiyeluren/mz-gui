package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestBackupRestoreAndVerifySQLiteFile(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "live", "app.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := Open(context.Background(), Options{
		Path: databasePath,
		Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE values_table (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO values_table(id, value) VALUES ('one', 'original');`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO values_table(id, value) VALUES ('two', 'from-wal')"); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "backup", "app.db")
	backup, err := Backup(context.Background(), database, BackupOptions{
		SourcePath: databasePath, DestinationPath: backupPath, MaxBytes: 16 << 20,
	})
	if err != nil {
		t.Fatalf("备份 SQLite: %v", err)
	}
	if !strings.HasPrefix(backup.SHA256, "sha256:") || backup.Size <= 0 || backup.Path != backupPath {
		t.Fatalf("备份快照无效: %#v", backup)
	}
	if info, err := os.Stat(backupPath); err != nil || !IsRestrictedFileMode(info.Mode()) {
		t.Fatalf("备份权限错误: info=%v err=%v", info, err)
	}
	if err := Close(database); err != nil {
		t.Fatal(err)
	}

	restoredPath := filepath.Join(root, "restore", "app.db")
	restored, err := Restore(context.Background(), RestoreOptions{
		BackupPath: backupPath, TargetPath: restoredPath, QuarantinePath: filepath.Join(root, "restore", "old.db"), MaxBytes: 16 << 20,
	})
	if err != nil {
		t.Fatalf("恢复 SQLite: %v", err)
	}
	if restored.SHA256 != backup.SHA256 {
		t.Fatalf("恢复 checksum 不一致: got=%s want=%s", restored.SHA256, backup.SHA256)
	}
	opened, err := Open(context.Background(), Options{Path: restoredPath, Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE values_table (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO values_table(id, value) VALUES ('one', 'original');`)}}})
	if err != nil {
		t.Fatalf("恢复后打开 SQLite: %v", err)
	}
	defer opened.Close()
	var count int
	if err := opened.QueryRow("SELECT COUNT(*) FROM values_table").Scan(&count); err != nil || count != 2 {
		t.Fatalf("恢复数据不完整: count=%d err=%v", count, err)
	}
}

func TestVerifyRejectsCorruptBackupAndRestoreQuarantinesOldTarget(t *testing.T) {
	root := t.TempDir()
	corruptPath := filepath.Join(root, "corrupt.db")
	if err := os.WriteFile(corruptPath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyFile(context.Background(), corruptPath, 4096); err == nil {
		t.Fatal("损坏 SQLite 文件必须被拒绝")
	}

	sourcePath := filepath.Join(root, "source.db")
	source, err := Open(context.Background(), Options{
		Path:       sourcePath,
		Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte("CREATE TABLE values_table (id TEXT PRIMARY KEY);")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "source.backup.db")
	if _, err := Backup(context.Background(), source, BackupOptions{SourcePath: sourcePath, DestinationPath: backupPath, MaxBytes: 16 << 20}); err != nil {
		t.Fatal(err)
	}
	if err := Close(source); err != nil {
		t.Fatal(err)
	}

	targetPath := filepath.Join(root, "target.db")
	target, err := Open(context.Background(), Options{
		Path:       targetPath,
		Migrations: fstest.MapFS{"0001_old.sql": &fstest.MapFile{Data: []byte("CREATE TABLE old_table (id TEXT PRIMARY KEY);")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Close(target); err != nil {
		t.Fatal(err)
	}
	quarantinePath := filepath.Join(root, "target.quarantine.db")
	if _, err := Restore(context.Background(), RestoreOptions{
		BackupPath: backupPath, TargetPath: targetPath, QuarantinePath: quarantinePath, MaxBytes: 16 << 20,
	}); err != nil {
		t.Fatalf("恢复覆盖目标: %v", err)
	}
	if _, err := os.Stat(quarantinePath); err != nil {
		t.Fatalf("旧目标未进入 quarantine: %v", err)
	}
	if _, err := VerifyFile(context.Background(), targetPath, 16<<20); err != nil {
		t.Fatalf("恢复目标校验失败: %v", err)
	}
}

func TestBackupRestoreValidateArguments(t *testing.T) {
	if _, err := Backup(nil, nil, BackupOptions{}); err == nil {
		t.Fatal("空 backup 参数必须拒绝")
	}
	if _, err := Restore(context.Background(), RestoreOptions{BackupPath: "/tmp/a.db", TargetPath: "/tmp/a.db", QuarantinePath: "/tmp/b.db"}); err == nil {
		t.Fatal("重复 restore 路径必须拒绝")
	}
	if _, err := VerifyFile(nil, "/tmp/missing.db", 0); err == nil {
		t.Fatal("空 verify context 必须拒绝")
	}
}
