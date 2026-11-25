package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestRecoverIfCorruptRestoresFromBackupAndQuarantinesSidecars(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "live", "app.db")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := Open(context.Background(), Options{
		Path: targetPath,
		Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE values_table (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO values_table(id, value) VALUES ('keep', 'backup-data');`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "backup", "app.db")
	if _, err := Backup(context.Background(), database, BackupOptions{
		SourcePath: targetPath, DestinationPath: backupPath, MaxBytes: 16 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Close(database); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath+"-wal", []byte("stale-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}

	quarantinePath := filepath.Join(root, "quarantine", "app.db")
	result, err := RecoverIfCorrupt(context.Background(), RecoverOptions{
		TargetPath: targetPath, BackupPath: backupPath, QuarantinePath: quarantinePath, MaxBytes: 16 << 20,
	})
	if err != nil || result.Action != RecoverActionRestored {
		t.Fatalf("损坏库应恢复: %#v err=%v", result, err)
	}
	if _, err := os.Stat(quarantinePath); err != nil {
		t.Fatalf("损坏库未隔离: %v", err)
	}
	if _, err := os.Stat(quarantinePath + "-wal"); err != nil {
		t.Fatalf("损坏 WAL 未隔离: %v", err)
	}
	opened, err := Open(context.Background(), Options{
		Path: targetPath,
		Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE values_table (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO values_table(id, value) VALUES ('keep', 'backup-data');`)}},
	})
	if err != nil {
		t.Fatalf("恢复后打开失败: %v", err)
	}
	defer opened.Close()
	var value string
	if err := opened.QueryRow("SELECT value FROM values_table WHERE id='keep'").Scan(&value); err != nil || value != "backup-data" {
		t.Fatalf("恢复数据错误: value=%q err=%v", value, err)
	}
}

func TestRecoverIfCorruptLeavesHealthyDatabaseAndFlagsUnrecoverable(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "healthy.db")
	database, err := Open(context.Background(), Options{
		Path:       targetPath,
		Migrations: fstest.MapFS{"0001_values.sql": &fstest.MapFile{Data: []byte("CREATE TABLE values_table (id TEXT PRIMARY KEY);")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Close(database); err != nil {
		t.Fatal(err)
	}
	result, err := RecoverIfCorrupt(context.Background(), RecoverOptions{
		TargetPath: targetPath, BackupPath: filepath.Join(root, "missing.db"),
		QuarantinePath: filepath.Join(root, "q.db"), MaxBytes: 16 << 20,
	})
	if err != nil || result.Action != RecoverActionNone {
		t.Fatalf("健康库不得恢复: %#v err=%v", result, err)
	}

	corruptPath := filepath.Join(root, "corrupt.db")
	if err := os.WriteFile(corruptPath, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = RecoverIfCorrupt(context.Background(), RecoverOptions{
		TargetPath: corruptPath, BackupPath: filepath.Join(root, "also-missing.db"),
		QuarantinePath: filepath.Join(root, "isolated.db"), MaxBytes: 16 << 20,
	})
	if err != nil || result.Action != RecoverActionRepairRequired {
		t.Fatalf("无可用备份应标记人工修复: %#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "isolated.db")); err != nil {
		t.Fatalf("不可恢复损坏库应隔离: %v", err)
	}
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatal("隔离后原路径应不存在")
	}
}

func TestRecoverIfCorruptRejectsInvalidOptions(t *testing.T) {
	if _, err := RecoverIfCorrupt(nil, RecoverOptions{}); err == nil {
		t.Fatal("空 context 必须拒绝")
	}
	if _, err := RecoverIfCorrupt(context.Background(), RecoverOptions{
		TargetPath: "/tmp/a.db", BackupPath: "/tmp/a.db", QuarantinePath: "/tmp/b.db",
	}); err == nil {
		t.Fatal("重复路径必须拒绝")
	}
}
