package migrations

import (
	"context"
	"database/sql"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

var legacyGrantTables = []frameworksqlite.LegacyTableMapping{{
	Source: "capability_grants", Target: "mzfw_capability_grants",
	Columns: []string{"grant_id", "subject", "capability", "scope_json", "risk_level", "granted_at",
		"expires_at", "revoked_at", "grant_source", "revision"},
}}

// ReconcileLegacy 将旧无前缀 capability grant 表幂等复制到 Framework 技术表并逐行校验。
func ReconcileLegacy(ctx context.Context, database *sql.DB) error {
	return frameworksqlite.ReconcileLegacyTables(ctx, database, legacyGrantTables)
}
