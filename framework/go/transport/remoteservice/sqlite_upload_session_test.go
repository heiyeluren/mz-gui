package remoteservice

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteUploadSessionStorePersistsStateAcrossReopen(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "upload-session.db")
	store := openSQLiteUploadSessionStore(t, databasePath, fixedClock{now: time.Unix(700, 0)})
	ctx := context.Background()
	state, err := NewUploadSessionState("upload-1", 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	state.MarkChunkCompleted(UploadChunk{Index: 0, Start: 0, End: 3, Total: 10}, time.Unix(701, 0).UTC())
	state.MarkChunkCompleted(UploadChunk{Index: 2, Start: 8, End: 9, Total: 10}, time.Unix(702, 0).UTC())
	if err := store.SaveUploadSession(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openSQLiteUploadSessionStore(t, databasePath, fixedClock{now: time.Unix(800, 0)})
	defer reopened.Close()
	loaded, found, err := reopened.LoadUploadSession(ctx, "upload-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("重新打开后应找到上传 session")
	}
	if loaded.SessionID != "upload-1" || loaded.TotalBytes != 10 || loaded.ChunkSize != 4 || loaded.TotalChunks != 3 {
		t.Fatalf("session 元数据未恢复: %#v", loaded)
	}
	if !loaded.IsChunkCompleted(0) || !loaded.IsChunkCompleted(2) || loaded.IsChunkCompleted(1) {
		t.Fatalf("完成分片未恢复: %#v", loaded.CompletedChunks)
	}
	if loaded.CompletedBytes() != 6 {
		t.Fatalf("完成字节数异常: %d", loaded.CompletedBytes())
	}
}

func TestSQLiteUploadSessionStoreReplacesExistingState(t *testing.T) {
	store := openSQLiteUploadSessionStore(t, filepath.Join(t.TempDir(), "upload-session.db"), fixedClock{now: time.Unix(900, 0)})
	defer store.Close()
	ctx := context.Background()
	first, err := NewUploadSessionState("upload-replace", 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	first.MarkChunkCompleted(UploadChunk{Index: 0, Start: 0, End: 3, Total: 8}, time.Unix(901, 0).UTC())
	if err := store.SaveUploadSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := NewUploadSessionState("upload-replace", 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	second.MarkChunkCompleted(UploadChunk{Index: 1, Start: 4, End: 7, Total: 8}, time.Unix(902, 0).UTC())
	if err := store.SaveUploadSession(ctx, second); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := store.LoadUploadSession(ctx, "upload-replace")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("应找到替换后的 session")
	}
	if loaded.IsChunkCompleted(0) || !loaded.IsChunkCompleted(1) || len(loaded.CompletedChunks) != 1 {
		t.Fatalf("同一 session 应使用最新状态替换旧状态: %#v", loaded.CompletedChunks)
	}
}

func TestSQLiteUploadSessionStoreRejectsInvalidSessionID(t *testing.T) {
	store := openSQLiteUploadSessionStore(t, filepath.Join(t.TempDir(), "upload-session.db"), fixedClock{now: time.Unix(1000, 0)})
	defer store.Close()
	if _, _, err := store.LoadUploadSession(context.Background(), ""); !errors.Is(err, ErrInvalidUpload) {
		t.Fatalf("空 session id 应拒绝: %v", err)
	}
}

func openSQLiteUploadSessionStore(t *testing.T, path string, clock fixedClock) *SQLiteUploadSessionStore {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteUploadSessionStore(database, SQLiteUploadSessionStoreOptions{Clock: clock})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	return store
}
