package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func TestOpenConfiguresBusyTimeoutForEveryConnection(t *testing.T) {
	database, err := Open(context.Background(), Options{
		Path: filepath.Join(t.TempDir(), "timeout.db"),
		Migrations: fstest.MapFS{
			"0001_values.sql": &fstest.MapFile{Data: []byte("CREATE TABLE values_table (id TEXT PRIMARY KEY);")},
		},
		BusyTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Close(database)
	for index := 0; index < 4; index++ {
		var timeout int
		if err := database.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 2000 {
			t.Fatalf("连接 %d 的 busy_timeout=%d，要求 2000: %v", index, timeout, err)
		}
	}
}

func TestOpenRejectsInvalidBusyTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{-time.Second, 31 * time.Second} {
		if _, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "invalid.db"), BusyTimeout: timeout}); err == nil {
			t.Fatalf("非法 busy_timeout %s 必须拒绝", timeout)
		}
	}
}
