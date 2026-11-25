package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenAppliesProvidedMigrationsAndSafetyDefaults(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "framework.db")
	database, err := Open(context.Background(), Options{
		Path: databasePath,
		Migrations: fstest.MapFS{
			"0001_widgets.sql":     &fstest.MapFile{Data: []byte("CREATE TABLE widgets (id TEXT PRIMARY KEY);")},
			"0002_widget_name.sql": &fstest.MapFile{Data: []byte("ALTER TABLE widgets ADD COLUMN name TEXT;")},
		},
		Now: func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("打开 Framework SQLite: %v", err)
	}
	defer Close(database)

	for name, expected := range map[string]string{"foreign_keys": "1", "journal_mode": "wal"} {
		var actual string
		if err := database.QueryRow("PRAGMA " + name).Scan(&actual); err != nil || actual != expected {
			t.Fatalf("PRAGMA %s=%q err=%v", name, actual, err)
		}
	}
	var migrations int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrations); err != nil || migrations != 2 {
		t.Fatalf("迁移记录错误: count=%d err=%v", migrations, err)
	}
	if info, err := os.Stat(databasePath); err != nil || !IsRestrictedFileMode(info.Mode()) {
		t.Fatalf("数据库权限不安全: info=%v err=%v", info, err)
	}
}

func TestOpenRejectsInvalidOptionsAndMigrationDrift(t *testing.T) {
	if _, err := Open(nil, Options{}); err == nil {
		t.Fatal("空 context 必须拒绝")
	}
	if _, err := Open(context.Background(), Options{}); err == nil {
		t.Fatal("空配置必须拒绝")
	}
	databasePath := filepath.Join(t.TempDir(), "drift.db")
	first := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE values_table (id TEXT);")}}
	database, err := Open(context.Background(), Options{Path: databasePath, Migrations: first})
	if err != nil {
		t.Fatal(err)
	}
	if err := Close(database); err != nil {
		t.Fatal(err)
	}

	changed := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE changed_table (id TEXT);")}}
	if _, err := Open(context.Background(), Options{Path: databasePath, Migrations: changed}); err == nil {
		t.Fatal("已应用迁移的 checksum 漂移必须拒绝")
	}
}

func TestSQLiteBusyHonorsCallerCancellation(t *testing.T) {
	database, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "busy.db"),
		Migrations: fstest.MapFS{
			"0001_values.sql": &fstest.MapFile{Data: []byte("CREATE TABLE values_table (id TEXT PRIMARY KEY);")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Close(database)

	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("INSERT INTO values_table(id) VALUES ('locked')"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, err = database.ExecContext(ctx, "INSERT INTO values_table(id) VALUES ('blocked')")
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("SQLite 写锁必须由调用方 deadline 取消: err=%v ctx=%v", err, ctx.Err())
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("SQLite busy 取消过慢: %s", elapsed)
	}
}
