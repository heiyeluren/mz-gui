package remoteservice

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteOutboxPersistsReadyEntryAcrossReopen(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "remote-outbox.db")
	clock := fixedClock{now: time.Unix(500, 0)}
	store := openSQLiteOutbox(t, databasePath, clock)
	ctx := context.Background()

	_, err := store.Enqueue(ctx, OutboxEntry{
		ID:        "entry-1",
		Operation: "sync-offline",
		Request: ReplayRequest{
			Method:    http.MethodGet,
			URL:       "https://api.example.test/v1/sync",
			Headers:   http.Header{"Accept": []string{"application/json"}},
			RequestID: "req-1",
		},
		NextAt: clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openSQLiteOutbox(t, databasePath, clock)
	defer reopened.Close()
	ready, err := reopened.ListReady(ctx, clock.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryIDs(ready); len(got) != 1 || got[0] != "entry-1" {
		t.Fatalf("reopened ready entries = %v", got)
	}
	if ready[0].Request.Headers.Get("Accept") != "application/json" || ready[0].Request.RequestID != "req-1" {
		t.Fatalf("entry 元数据未恢复: %#v", ready[0])
	}
}

func TestSQLiteOutboxReplayerPersistsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	clock := fixedClock{now: time.Unix(600, 0)}
	store := openSQLiteOutbox(t, filepath.Join(t.TempDir(), "remote-outbox.db"), clock)
	defer store.Close()
	ctx := context.Background()
	_, err := store.Enqueue(ctx, OutboxEntry{
		ID:        "entry-1",
		Operation: "sync",
		Request:   ReplayRequest{Method: http.MethodGet, URL: server.URL},
		NextAt:    clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	replayer := NewOutboxReplayer(OutboxReplayerOptions{Store: store, Client: client, Clock: clock})
	result, err := replayer.ReplayReady(ctx, ReplayOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 1 {
		t.Fatalf("replay result = %#v", result)
	}
	entry, err := store.Get(ctx, "entry-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Status != OutboxStatusSucceeded || entry.Attempts != 1 {
		t.Fatalf("持久化成功状态错误: %#v", entry)
	}
}

func openSQLiteOutbox(t *testing.T, path string, clock fixedClock) *SQLiteOutbox {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteOutbox(database, SQLiteOutboxOptions{Clock: clock})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	return store
}
