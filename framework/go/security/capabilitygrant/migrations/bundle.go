package migrations

import frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"

const Owner = "framework.security.capability"

// Bundle 返回 capability grant 技术 schema 的迁移声明。
func Bundle() frameworksqlite.MigrationBundle {
	return frameworksqlite.MigrationBundle{
		Owner:      Owner,
		Migrations: Files,
		Objects: []frameworksqlite.SchemaObject{
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_capability_grants"},
			{Type: frameworksqlite.SchemaObjectIndex, Name: "mzfw_capability_grants_subject_capability_active_idx"},
		},
	}
}
