package windowstate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreSavesAndRestoresByWindowID(t *testing.T) {
	store := NewMemoryStore(Options{MaxPayloadBytes: 1024})
	saved := Snapshot{
		WindowID: "win-main",
		RouteID:  "chat",
		Revision: 3,
		Payload:  []byte(`{"draft":"hello"}`),
		SavedAt:  time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC),
	}
	if err := store.Save(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "win-main")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WindowID != "win-main" || loaded.RouteID != "chat" || loaded.Revision != 3 || string(loaded.Payload) != `{"draft":"hello"}` {
		t.Fatalf("恢复快照异常: %#v", loaded)
	}
}

func TestMemoryStoreRejectsStaleRevisionAndAcceptsNewer(t *testing.T) {
	store := NewMemoryStore(Options{})
	first := Snapshot{WindowID: "win-1", RouteID: "editor", Revision: 2, Payload: []byte("v2"), SavedAt: time.Now()}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", RouteID: "editor", Revision: 1, Payload: []byte("v1"), SavedAt: time.Now()}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("旧 revision 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", RouteID: "editor", Revision: 4, Payload: []byte("v4"), SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "win-1")
	if err != nil || loaded.Revision != 4 || string(loaded.Payload) != "v4" {
		t.Fatalf("新 revision 未覆盖: %#v %v", loaded, err)
	}
}

func TestMemoryStoreRejectsInvalidSnapshotAndMissingWindow(t *testing.T) {
	store := NewMemoryStore(Options{MaxPayloadBytes: 8})
	if _, err := store.Load(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失窗口应 not found: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "", Revision: 1, Payload: []byte("x")}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("空 WindowID 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", Revision: 0, Payload: []byte("x")}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("revision 0 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", Revision: 1, Payload: []byte("0123456789")}); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("超大 payload 必须拒绝: %v", err)
	}
	if err := store.Save(context.Background(), Snapshot{WindowID: "win-1", Revision: 1, Payload: []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), "win-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), "win-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后应 not found: %v", err)
	}
}
