package migrations

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestBundleOwnsAuditSchemaAndReconcilesLegacy(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(t.TempDir(), "audit.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			Bundle(),
			{
				Owner:        "application.legacy-audit-test",
				Dependencies: []string{Owner},
				Migrations: fstest.MapFS{"0001_audit.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE audit_events (
audit_id TEXT PRIMARY KEY,
occurred_at TEXT NOT NULL,
actor TEXT NOT NULL,
action TEXT NOT NULL,
capability TEXT,
resource_type TEXT,
resource_id TEXT,
decision TEXT NOT NULL,
reason_code TEXT,
request_id TEXT,
metadata_json TEXT NOT NULL
);`)}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_audit_events'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("Audit schema owner 错误: owner=%q err=%v", owner, err)
	}
	if _, err := database.Exec(`INSERT INTO audit_events (
audit_id, occurred_at, actor, action, capability, resource_type, resource_id, decision, reason_code, request_id, metadata_json
) VALUES ('audit-legacy', '2026-08-15T00:00:00Z', 'desktop', 'workspace.grant.create',
'filesystem.grant', 'workspace_grant', 'grant-legacy', 'allowed', 'user_selected', 'request-1', '{}')`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("复制旧 Audit 数据: %v", err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("重复 reconcile 必须幂等: %v", err)
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM mzfw_audit_events WHERE audit_id='audit-legacy'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("旧 audit 未复制: count=%d err=%v", count, err)
	}
	if _, err := database.Exec("UPDATE mzfw_audit_events SET decision='denied' WHERE audit_id='audit-legacy'"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err == nil {
		t.Fatal("Audit event 同主键异内容必须拒绝切换")
	}
}

func TestReconcileLegacyRejectsInvalidArguments(t *testing.T) {
	if err := ReconcileLegacy(nil, nil); err == nil {
		t.Fatal("空参数必须拒绝")
	}
}
