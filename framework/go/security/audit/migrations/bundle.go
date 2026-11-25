package migrations

import frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"

const Owner = "framework.security.audit"

// Bundle 返回 audit event 技术 schema 的迁移声明。
func Bundle() frameworksqlite.MigrationBundle {
	return frameworksqlite.MigrationBundle{
		Owner:      Owner,
		Migrations: Files,
		Objects: []frameworksqlite.SchemaObject{
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_audit_events"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_audit_events_resource_occurred_at_idx"},
		},
	}
}
