package migrations

import (
	"context"
	"path/filepath"
	"testing"

	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestBundleOwnsWindowStateSchema(t *testing.T) {
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path:    filepath.Join(t.TempDir(), "window-state.db"),
		Bundles: []frameworksqlite.MigrationBundle{Bundle()},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var owner string
	if err := database.QueryRow("SELECT owner FROM mzfw_schema_objects WHERE object_name = 'mzfw_window_state'").Scan(&owner); err != nil || owner != Owner {
		t.Fatalf("window state schema owner 错误: owner=%q err=%v", owner, err)
	}
}
