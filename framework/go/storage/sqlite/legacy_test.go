package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestReconcileLegacyTablesCopiesIdempotentlyAndRejectsDrift(t *testing.T) {
	database, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "legacy.db"),
		Migrations: fstest.MapFS{"0001_tables.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE old_values (id TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE new_values (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO old_values(id, value) VALUES ('one', 'old');`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	mappings := []LegacyTableMapping{{Source: "old_values", Target: "new_values", Columns: []string{"id", "value"}}}
	if err := ReconcileLegacyTables(context.Background(), database, mappings); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacyTables(context.Background(), database, mappings); err != nil {
		t.Fatalf("重复 reconcile 必须幂等: %v", err)
	}
	if _, err := database.Exec("UPDATE new_values SET value='drift' WHERE id='one'"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacyTables(context.Background(), database, mappings); err == nil {
		t.Fatal("同主键异内容必须拒绝")
	}
}

func TestReconcileLegacyTablesValidatesMappingAndCompleteness(t *testing.T) {
	if err := ReconcileLegacyTables(nil, nil, nil); err == nil {
		t.Fatal("空参数必须拒绝")
	}
	database, err := Open(context.Background(), Options{
		Path:       filepath.Join(t.TempDir(), "partial.db"),
		Migrations: fstest.MapFS{"0001_old.sql": &fstest.MapFile{Data: []byte("CREATE TABLE old_one (id TEXT);")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := ReconcileLegacyTables(context.Background(), database, []LegacyTableMapping{{Source: "bad name", Target: "new_one", Columns: []string{"id"}}}); err == nil {
		t.Fatal("非法 mapping 必须拒绝")
	}
	if err := ReconcileLegacyTables(context.Background(), database, []LegacyTableMapping{
		{Source: "old_one", Target: "new_one", Columns: []string{"id"}},
		{Source: "old_two", Target: "new_two", Columns: []string{"id"}},
	}); err == nil {
		t.Fatal("不完整旧 schema 必须拒绝")
	}
}
