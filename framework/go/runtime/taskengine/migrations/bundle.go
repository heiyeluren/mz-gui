package migrations

import frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"

const Owner = "framework.runtime.taskengine"

// Bundle 返回 Task Engine 技术 schema 的迁移声明。
func Bundle() frameworksqlite.MigrationBundle {
	return frameworksqlite.MigrationBundle{
		Owner:      Owner,
		Migrations: Files,
		Objects: []frameworksqlite.SchemaObject{
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_task_checkpoints"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_task_checkpoints_latest_idx"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_tasks"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_task_attempts"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_task_output"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_task_idempotency"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_tasks_idempotency_key_uq"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_tasks_state_scheduled_at_idx"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_tasks_next_retry_at_idx"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_tasks_workspace_created_at_idx"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_task_idempotency_task_created_at_idx"},
		},
	}
}
