package sqlite

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestOpenAppliesMigrationBundlesInDependencyOrderAndRegistersOwners(t *testing.T) {
	database, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "bundles.db"),
		Bundles: []MigrationBundle{
			{
				Owner:        "application.example",
				Dependencies: []string{"framework.runtime.taskengine"},
				Migrations: fstest.MapFS{
					"0001_notes.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example_notes (id TEXT PRIMARY KEY);")},
				},
				Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "example_notes"}},
			},
			{
				Owner: "framework.runtime.taskengine",
				Migrations: fstest.MapFS{
					"0001_tasks.sql": &fstest.MapFile{Data: []byte("CREATE TABLE mzfw_tasks (id TEXT PRIMARY KEY);")},
				},
				Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_tasks"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("打开组合迁移数据库: %v", err)
	}
	defer database.Close()

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM mzfw_schema_migrations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("组合迁移记录错误: count=%d err=%v", count, err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM mzfw_schema_objects").Scan(&count); err != nil || count != 2 {
		t.Fatalf("对象归属记录错误: count=%d err=%v", count, err)
	}
	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_tasks'").Scan(&owner); err != nil || owner != "framework.runtime.taskengine" {
		t.Fatalf("Framework 对象 owner 错误: owner=%q err=%v", owner, err)
	}
}

func TestOpenRejectsInvalidBundleGraphAndObjectOwnershipConflict(t *testing.T) {
	tests := []struct {
		name    string
		bundles []MigrationBundle
	}{
		{
			name: "缺少依赖",
			bundles: []MigrationBundle{{
				Owner: "application.example", Dependencies: []string{"framework.missing"},
				Migrations: fstest.MapFS{"0001_app.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
			}},
		},
		{
			name: "循环依赖",
			bundles: []MigrationBundle{
				{Owner: "framework.one", Dependencies: []string{"framework.two"}, Migrations: fstest.MapFS{"0001_one.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}},
				{Owner: "framework.two", Dependencies: []string{"framework.one"}, Migrations: fstest.MapFS{"0001_two.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}},
			},
		},
		{
			name: "对象重复归属",
			bundles: []MigrationBundle{
				{Owner: "framework.one", Migrations: fstest.MapFS{"0001_one.sql": &fstest.MapFile{Data: []byte("CREATE TABLE mzfw_shared (id TEXT);")}}, Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_shared"}}},
				{Owner: "framework.two", Migrations: fstest.MapFS{"0001_two.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}, Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_shared"}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "invalid.db"), Bundles: test.bundles}); err == nil {
				t.Fatal("无效 bundle 必须拒绝")
			}
		})
	}
}

func TestOpenAdoptsLegacyMigrationRecordsWithoutReapplyingSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	migrations := fstest.MapFS{
		"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example_data (id TEXT PRIMARY KEY);")},
	}
	legacy, err := Open(context.Background(), Options{Path: path, Migrations: migrations})
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	database, err := Open(context.Background(), Options{
		Path: path,
		Bundles: []MigrationBundle{{
			Owner: "application.example", Migrations: migrations, AdoptLegacy: true,
			Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "example_data"}},
		}},
	})
	if err != nil {
		t.Fatalf("接管旧迁移记录: %v", err)
	}
	defer database.Close()

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM mzfw_schema_migrations WHERE owner = 'application.example'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("旧迁移接管记录错误: count=%d err=%v", count, err)
	}
}

func TestOpenRejectsBundleChecksumDriftAndMissingDeclaredObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drift.db")
	first := MigrationBundle{
		Owner:      "framework.example",
		Migrations: fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE mzfw_values (id TEXT);")}},
		Objects:    []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_values"}},
	}
	database, err := Open(context.Background(), Options{Path: path, Bundles: []MigrationBundle{first}})
	if err != nil {
		t.Fatal(err)
	}
	database.Close()

	changed := first
	changed.Migrations = fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE mzfw_changed (id TEXT);")}}
	if _, err := Open(context.Background(), Options{Path: path, Bundles: []MigrationBundle{changed}}); err == nil {
		t.Fatal("已应用 bundle checksum 漂移必须拒绝")
	}

	missing := MigrationBundle{
		Owner:      "framework.missing",
		Migrations: fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		Objects:    []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_not_created"}},
	}
	if _, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "missing.db"), Bundles: []MigrationBundle{missing}}); err == nil {
		t.Fatal("未创建的声明对象必须拒绝")
	}
}

func TestPlanBundlesUsesStableOwnerOrder(t *testing.T) {
	filesystem := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}
	planned, err := planBundles([]MigrationBundle{
		{Owner: "application.example", Migrations: filesystem},
		{Owner: "adapter.remote", Migrations: filesystem},
		{Owner: "framework.zeta", Migrations: filesystem},
		{Owner: "framework.alpha", Migrations: filesystem},
	})
	if err != nil {
		t.Fatal(err)
	}
	owners := make([]string, 0, len(planned))
	for _, bundle := range planned {
		owners = append(owners, bundle.Owner)
	}
	want := []string{"framework.alpha", "framework.zeta", "adapter.remote", "application.example"}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("bundle 顺序不稳定: got=%v want=%v", owners, want)
	}
}

func TestPlanBundlesRejectsInvalidMetadata(t *testing.T) {
	filesystem := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}
	tests := []MigrationBundle{
		{Owner: "invalid", Migrations: filesystem},
		{Owner: "framework.example"},
		{Owner: "framework.example", Migrations: filesystem, Dependencies: []string{"bad"}},
		{Owner: "framework.example", Migrations: filesystem, Dependencies: []string{"adapter.same", "adapter.same"}},
		{Owner: "framework.example", Migrations: filesystem, Objects: []SchemaObject{{Type: "bad", Name: "mzfw_values"}}},
		{Owner: "framework.example", Migrations: filesystem, Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "values"}}},
		{Owner: "application.example", Migrations: filesystem, Objects: []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_values"}}},
	}
	for index, bundle := range tests {
		if _, err := planBundles([]MigrationBundle{bundle}); err == nil {
			t.Fatalf("第 %d 个无效 bundle 必须拒绝", index)
		}
	}
	if _, err := planBundles([]MigrationBundle{
		{Owner: "framework.same", Migrations: filesystem},
		{Owner: "framework.same", Migrations: filesystem},
	}); err == nil {
		t.Fatal("重复 owner 必须拒绝")
	}
}

func TestMigrationVersionValidation(t *testing.T) {
	if version, err := migrationVersion("0007_valid.sql"); err != nil || version != 7 {
		t.Fatalf("有效版本解析失败: version=%d err=%v", version, err)
	}
	for _, name := range []string{"invalid.sql", "zero_initial.sql", "0000_initial.sql"} {
		if _, err := migrationVersion(name); err == nil {
			t.Fatalf("无效迁移名必须拒绝: %s", name)
		}
	}
}

func TestOpenRejectsDuplicateVersionFailedSQLLegacyMismatchAndOwnerDrift(t *testing.T) {
	if _, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "duplicate.db"),
		Bundles: []MigrationBundle{{Owner: "framework.example", Migrations: fstest.MapFS{
			"0001_first.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			"1_second.sql":   &fstest.MapFile{Data: []byte("SELECT 2;")},
		}}},
	}); err == nil {
		t.Fatal("同一 bundle 重复版本必须拒绝")
	}
	if _, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "failed.db"),
		Bundles: []MigrationBundle{{Owner: "framework.example", Migrations: fstest.MapFS{
			"0001_failed.sql": &fstest.MapFile{Data: []byte("INVALID SQL")},
		}}},
	}); err == nil {
		t.Fatal("迁移 SQL 失败必须返回错误")
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy-mismatch.db")
	legacyMigrations := fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE old_values (id TEXT);")}}
	legacy, err := Open(context.Background(), Options{Path: legacyPath, Migrations: legacyMigrations})
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	if _, err := Open(context.Background(), Options{
		Path: legacyPath,
		Bundles: []MigrationBundle{{Owner: "application.example", AdoptLegacy: true, Migrations: fstest.MapFS{
			"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE changed_values (id TEXT);")},
		}}},
	}); err == nil {
		t.Fatal("旧迁移 checksum 不一致必须拒绝接管")
	}

	driftPath := filepath.Join(t.TempDir(), "owner-drift.db")
	bundle := MigrationBundle{
		Owner:      "framework.example",
		Migrations: fstest.MapFS{"0001_initial.sql": &fstest.MapFile{Data: []byte("CREATE TABLE mzfw_values (id TEXT);")}},
		Objects:    []SchemaObject{{Type: SchemaObjectTable, Name: "mzfw_values"}},
	}
	database, err := Open(context.Background(), Options{Path: driftPath, Bundles: []MigrationBundle{bundle}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE mzfw_schema_objects SET owner = 'framework.other' WHERE object_name = 'mzfw_values'"); err != nil {
		t.Fatal(err)
	}
	database.Close()
	if _, err := Open(context.Background(), Options{Path: driftPath, Bundles: []MigrationBundle{bundle}}); err == nil {
		t.Fatal("已登记对象 owner 漂移必须拒绝")
	}
}
