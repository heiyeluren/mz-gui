package migrations

import (
	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

const Owner = "framework.runtime.scheduler"

// Bundle 返回 Scheduler 技术 schema 的迁移声明。
func Bundle() frameworksqlite.MigrationBundle {
	return frameworksqlite.MigrationBundle{
		Owner: Owner, Dependencies: []string{taskmigrations.Owner}, Migrations: Files,
		Objects: []frameworksqlite.SchemaObject{
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_schedules"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_schedule_occurrences"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_schedule_idempotency"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_schedules_next_run_at_idx"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_schedules_active_updated_at_idx"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_schedule_idempotency_schedule_created_at_idx"},
		},
	}
}
