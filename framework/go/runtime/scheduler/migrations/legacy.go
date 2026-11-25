package migrations

import (
	"context"
	"database/sql"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

var legacyScheduleTables = []frameworksqlite.LegacyTableMapping{
	{
		Source: "schedules", Target: "mzfw_schedules",
		Columns: []string{"schedule_id", "name", "enabled", "kind", "expression", "timezone", "misfire_policy",
			"overlap_policy", "jitter_ms", "next_run_at", "last_run_at", "payload_template", "revision",
			"max_catch_up", "overlap_limit", "created_at", "updated_at", "deleted_at"},
	},
	{
		Source: "schedule_occurrences", Target: "mzfw_schedule_occurrences",
		Columns: []string{"occurrence_id", "schedule_id", "planned_at", "task_id", "state", "created_at", "reason_code"},
	},
	{
		Source: "schedule_rpc_idempotency", Target: "mzfw_schedule_idempotency",
		Columns: []string{"idempotency_key", "operation", "request_fingerprint", "schedule_id", "occurrence_id",
			"result_revision", "created_at"},
	},
}

// ReconcileLegacy 将旧无前缀 Scheduler 表幂等复制到 Framework 技术表并逐行校验。
func ReconcileLegacy(ctx context.Context, database *sql.DB) error {
	return frameworksqlite.ReconcileLegacyTables(ctx, database, legacyScheduleTables)
}
