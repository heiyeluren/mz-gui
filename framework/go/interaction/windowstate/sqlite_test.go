package windowstate

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.db")
	store, err := OpenSQLiteStore(context.Background(), path, Options{MaxPayloadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	saved := Snapshot{
		WindowID: "win-main",
		RouteID:  "chat",
		Revision: 3,
		Payload:  []byte(`{"draft":"hello"}`),
		SavedAt:  time.Date(2026, 8, 16, 14, 30, 0, 0, time.UTC),
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteStore(context.Background(), path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err := reopened.Load(context.Background(), "win-main")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WindowID != "win-main" || loaded.RouteID != "chat" || loaded.Revision != 3 || string(loaded.Payload) != `{"draft":"hello"}` {
		t.Fatalf("重启后恢复异常: %#v", loaded)
	}
}

func TestSQLiteStoreRejectsStaleRevisionAndDeletes(t *testing.T) {
	store, err := OpenSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "window-state.db"), Options{MaxPayloadBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", RouteID: "editor", Revision: 2, Payload: []byte("v2"), SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", Revision: 1, Payload: []byte("v1")}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("旧 revision 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", Revision: 4, Payload: []byte("v4")}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "win-1")
	if err != nil || loaded.Revision != 4 || string(loaded.Payload) != "v4" {
		t.Fatalf("新 revision 未覆盖: %#v %v", loaded, err)
	}
	if _, err := store.Load(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失窗口应 not found: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "", Revision: 1, Payload: []byte("x")}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("空 WindowID 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-2", Revision: 1, Payload: []byte("0123456789")}); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("超大 payload 必须拒绝: %v", err)
	}
	if err := store.Delete(context.Background(), "win-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "win-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应 not found: %v", err)
	}
}

func TestSQLiteStoreRestoresPagesAcrossWindows(t *testing.T) {
	store, err := OpenSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "window-page.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "chat", Revision: 1, Payload: []byte("draft")}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestorePages(context.Background(), "win-a", "win-b"); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPage(context.Background(), "win-b", "chat")
	if err != nil || string(loaded.Payload) != "draft" || loaded.WindowID != "win-b" {
		t.Fatalf("SQLite 页面未跨窗口恢复: %#v %v", loaded, err)
	}
}

func TestOpenSQLiteStoreRejectsEmptyPath(t *testing.T) {
	if _, err := OpenSQLiteStore(context.Background(), "", Options{}); err == nil {
		t.Fatal("空路径必须拒绝")
	}
	if _, err := OpenSQLiteStore(nil, filepath.Join(t.TempDir(), "x.db"), Options{}); err == nil {
		t.Fatal("空 context 必须拒绝")
	}
}
