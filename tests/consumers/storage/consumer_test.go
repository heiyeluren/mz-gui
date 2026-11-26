package storageconsumer_test

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
	"github.com/heiyeluren/mz-gui/framework/go/transport/streamresume"
)

func TestSQLiteAndStreamUpgradeContracts(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, sqlite.Options{
		Path: filepath.Join(t.TempDir(), "consumer.db"),
		Bundles: []sqlite.MigrationBundle{{
			Owner:      "application.consumer",
			Migrations: fstest.MapFS{"0001_notes.sql": &fstest.MapFile{Data: []byte("CREATE TABLE notes (id TEXT PRIMARY KEY);")}},
			Objects:    []sqlite.SchemaObject{{Type: sqlite.SchemaObjectTable, Name: "notes"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "INSERT INTO notes(id) VALUES (?)", "one"); err != nil {
		t.Fatal(err)
	}

	streams, err := streamresume.New(streamresume.Options{MaxFramesPerStream: 4})
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := streams.Open(ctx, "consumer-stream")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := streams.Append(ctx, streamresume.AppendRequest{StreamID: cursor.StreamID, Sequence: 1, Payload: []byte("ready")}); err != nil {
		t.Fatal(err)
	}
	frames, _, err := streams.Replay(ctx, streamresume.ReplayRequest{Cursor: cursor, Limit: 4})
	if err != nil || len(frames) != 1 || string(frames[0].Payload) != "ready" {
		t.Fatalf("流恢复契约异常: frames=%v err=%v", frames, err)
	}
}
