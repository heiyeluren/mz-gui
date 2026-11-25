package migrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestBundleCreatesOwnedCheckpointSchema(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(t.TempDir(), "taskengine.db"), Bundles: []frameworksqlite.MigrationBundle{Bundle()},
	})
	if err != nil {
		t.Fatalf("应用 Task Engine bundle: %v", err)
	}
	defer database.Close()

	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_task_checkpoints'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("checkpoint schema owner 错误: owner=%q err=%v", owner, err)
	}
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_tasks'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("Task schema owner 错误: owner=%q err=%v", owner, err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("无旧表时 reconcile 应为空操作: %v", err)
	}
}

func TestReconcileLegacyCopiesEveryTaskTableAndRejectsDrift(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(t.TempDir(), "legacy-taskengine.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			Bundle(),
			{Owner: "application.legacy-task-test", Dependencies: []string{Owner}, Migrations: os.DirFS("../testdata")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`INSERT INTO tasks (
		task_id, task_type, state, priority, payload_version, payload_json, idempotency_key,
		required_capabilities_json, created_at, attempt_count, max_attempts, revision
	) VALUES ('task-legacy', 'test.echo', 'running', 3, 1, '{"value":1}', 'legacy-key',
		'[]', '2026-08-15T00:00:00Z', 1, 2, 4);
	INSERT INTO task_attempts (attempt_id, task_id, attempt_no, worker_id, started_at, output_start_cursor)
	VALUES ('attempt-legacy', 'task-legacy', 1, 'worker-1', '2026-08-15T00:00:01Z', 1);
	INSERT INTO task_output (task_id, cursor, kind, occurred_at, payload)
	VALUES ('task-legacy', 1, 'stdout', '2026-08-15T00:00:02Z', 'hello');
	INSERT INTO rpc_idempotency (idempotency_key, operation, request_fingerprint, task_id, result_revision, created_at)
	VALUES ('rpc-legacy', 'task.cancel', 'fingerprint', 'task-legacy', 4, '2026-08-15T00:00:03Z');`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("复制旧 Task 数据: %v", err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("重复 reconcile 必须幂等: %v", err)
	}
	for _, table := range []string{"mzfw_tasks", "mzfw_task_attempts", "mzfw_task_output", "mzfw_task_idempotency"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("旧数据未复制到 %s: count=%d err=%v", table, count, err)
		}
	}
	if _, err := database.Exec(`UPDATE mzfw_tasks SET payload_json='{"value":2}' WHERE task_id='task-legacy'`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err == nil {
		t.Fatal("同主键异内容必须拒绝切换")
	}
}

func TestReconcileLegacyRejectsInvalidArguments(t *testing.T) {
	if err := ReconcileLegacy(nil, nil); err == nil {
		t.Fatal("空参数必须拒绝")
	}
}
