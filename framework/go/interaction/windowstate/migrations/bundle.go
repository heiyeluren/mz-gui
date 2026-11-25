package migrations

import frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"

const Owner = "framework.interaction.windowstate"

func Bundle() frameworksqlite.MigrationBundle {
	return frameworksqlite.MigrationBundle{
		Owner:      Owner,
		Migrations: Files,
		Objects: []frameworksqlite.SchemaObject{
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_window_state"},
			{Type: frameworksqlite.SchemaObjectTable, Name: "mzfw_window_page_state"},
		},
	}
}
