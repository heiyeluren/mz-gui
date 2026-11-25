package migrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestBundleAndLegacyReconcileOwnSchedulerSchema(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(t.TempDir(), "scheduler.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			taskmigrations.Bundle(), Bundle(),
			{Owner: "application.legacy-scheduler-test", Dependencies: []string{Owner}, Migrations: os.DirFS("../testdata")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`INSERT INTO tasks (
		task_id, task_type, state, priority, payload_version, payload_json, idempotency_key,
		required_capabilities_json, created_at, attempt_count, max_attempts, revision
	) VALUES ('task-scheduled', 'test.echo', 'pending', 0, 1, '{}', 'scheduled-key',
		'[]', '2026-08-15T00:00:00Z', 0, 1, 1);
	INSERT INTO schedules (
		schedule_id, name, enabled, kind, expression, timezone, misfire_policy, overlap_policy,
		jitter_ms, payload_template, revision, max_catch_up, overlap_limit, created_at, updated_at
	) VALUES ('schedule-legacy', 'Legacy', 1, 'interval', '15m', 'UTC', 'run_once', 'forbid',
		0, '{}', 2, 1, 1, '2026-08-15T00:00:00Z', '2026-08-15T00:00:00Z');
	INSERT INTO schedule_occurrences (occurrence_id, schedule_id, planned_at, task_id, state, created_at)
	VALUES ('occurrence-legacy', 'schedule-legacy', '2026-08-15T00:15:00Z', 'task-scheduled', 'created', '2026-08-15T00:00:01Z');
	INSERT INTO schedule_rpc_idempotency (
		idempotency_key, operation, request_fingerprint, schedule_id, occurrence_id, result_revision, created_at
	) VALUES ('schedule-rpc', 'schedule.trigger', 'fingerprint', 'schedule-legacy', 'occurrence-legacy', 2, '2026-08-15T00:00:02Z');`); err != nil {
		t.Fatal(err)
	}
	if err := taskmigrations.ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("复制旧 Scheduler 数据: %v", err)
	}
	if err := ReconcileLegacy(context.Background(), database); err != nil {
		t.Fatalf("重复 reconcile 必须幂等: %v", err)
	}
	for _, table := range []string{"mzfw_schedules", "mzfw_schedule_occurrences", "mzfw_schedule_idempotency"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("旧数据未复制到 %s: count=%d err=%v", table, count, err)
		}
	}
	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name='mzfw_schedules'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("Scheduler schema owner 错误: owner=%q err=%v", owner, err)
	}
	if _, err := database.Exec("UPDATE mzfw_schedules SET name='drift' WHERE schedule_id='schedule-legacy'"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileLegacy(context.Background(), database); err == nil {
		t.Fatal("Scheduler 同主键异内容必须拒绝切换")
	}
}

func TestReconcileLegacyRejectsInvalidArguments(t *testing.T) {
	if err := ReconcileLegacy(nil, nil); err == nil {
		t.Fatal("空参数必须拒绝")
	}
}
