package migrations

import (
	"context"
	"database/sql"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

var legacyTaskTables = []frameworksqlite.LegacyTableMapping{
	{
		Source: "tasks", Target: "mzfw_tasks",
		Columns: []string{"task_id", "task_type", "state", "priority", "payload_version", "payload_json",
			"idempotency_key", "workspace_id", "required_capabilities_json", "created_at", "scheduled_at",
			"started_at", "finished_at", "lease_owner", "lease_expires_at", "attempt_count", "max_attempts",
			"next_retry_at", "cancel_requested_at", "result_summary_json", "error_code", "revision"},
	},
	{
		Source: "task_attempts", Target: "mzfw_task_attempts",
		Columns: []string{"attempt_id", "task_id", "attempt_no", "worker_id", "started_at", "finished_at",
			"outcome", "error_code", "error_summary", "output_start_cursor", "output_end_cursor"},
	},
	{Source: "task_output", Target: "mzfw_task_output", Columns: []string{"task_id", "cursor", "kind", "occurred_at", "payload"}},
	{
		Source: "rpc_idempotency", Target: "mzfw_task_idempotency",
		Columns: []string{"idempotency_key", "operation", "request_fingerprint", "task_id", "result_revision", "created_at"},
	},
}

// ReconcileLegacy 将旧无前缀 Task 表幂等复制到 Framework 技术表并逐行校验。
func ReconcileLegacy(ctx context.Context, database *sql.DB) error {
	return frameworksqlite.ReconcileLegacyTables(ctx, database, legacyTaskTables)
}
