package migrations

import (
	"context"
	"database/sql"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

var legacyAuditTables = []frameworksqlite.LegacyTableMapping{{
	Source: "audit_events", Target: "mzfw_audit_events",
	Columns: []string{"audit_id", "occurred_at", "actor", "action", "capability", "resource_type",
		"resource_id", "decision", "reason_code", "request_id", "metadata_json"},
}}

// ReconcileLegacy 将旧无前缀 audit event 表幂等复制到 Framework 技术表并逐行校验。
func ReconcileLegacy(ctx context.Context, database *sql.DB) error {
	return frameworksqlite.ReconcileLegacyTables(ctx, database, legacyAuditTables)
}
