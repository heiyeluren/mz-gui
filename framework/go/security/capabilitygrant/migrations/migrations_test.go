package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestBundleOwnsCapabilityGrantSchemaAndReconcilesLegacy(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(t.TempDir(), "capability.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			Bundle(),
			{
				Owner:        "application.legacy-capability-test",
				Dependencies: []string{Owner},
				Migrations: fstest.MapFS{"0001_capability.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE capability_grants (
grant_id TEXT PRIMARY KEY,
subject TEXT NOT NULL,
capability TEXT NOT NULL,
scope_json TEXT NOT NULL,
risk_level TEXT NOT NULL,
granted_at TEXT NOT NULL,
expires_at TEXT,
revoked_at TEXT,
grant_source TEXT NOT NULL,
revision INTEGER NOT NULL
);`)}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_capability_grants'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("Capability grant schema owner 错误: owner=%q err=%v", owner, err)
	}
	if _, err := database.Exec(`INSERT INTO capability_grants (
grant_id, subject, capability, scope_json, risk_level, granted_at, grant_source, revision
) VALUES ('grant-legacy', 'desktop', 'filesystem.read', '{"schema_version":1}', 'medium',
'2026-08-15T00:00:00Z', 'user_selected', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("复制旧 Capability grant 数据: %v", err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("重复 reconcile 必须幂等: %v", err)
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM mzfw_capability_grants WHERE grant_id='grant-legacy'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("旧 grant 未复制: count=%d err=%v", count, err)
	}
	if _, err := database.Exec("UPDATE mzfw_capability_grants SET revision=2 WHERE grant_id='grant-legacy'"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err == nil {
		t.Fatal("Capability grant 同主键异内容必须拒绝切换")
	}
}

func TestReconcileLegacyRejectsInvalidArguments(t *testing.T) {
	if err := ReconcileLegacy(nil, nil); err == nil {
		t.Fatal("空参数必须拒绝")
	}
}
