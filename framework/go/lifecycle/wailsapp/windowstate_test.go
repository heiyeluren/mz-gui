package wailsapp

import (
	"context"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
)

func TestPersistWindowStateWritesSnapshot(t *testing.T) {
	store := windowstate.NewMemoryStore(windowstate.Options{})
	if err := PersistWindowState(context.Background(), store, "win-main", "chat", `{"draft":"ok"}`); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "win-main")
	if err != nil || loaded.RouteID != "chat" || string(loaded.Payload) != `{"draft":"ok"}` || loaded.Revision != 1 {
		t.Fatalf("窗口状态未写入: %#v %v", loaded, err)
	}
	if err := PersistWindowState(context.Background(), store, "win-main", "chat", `{"draft":"v2"}`); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Load(context.Background(), "win-main")
	if err != nil || updated.Revision != 2 || string(updated.Payload) != `{"draft":"v2"}` {
		t.Fatalf("revision 未提升: %#v %v", updated, err)
	}
}

func TestPersistWindowStateRejectsInvalidInput(t *testing.T) {
	if err := PersistWindowState(context.Background(), nil, "win", "chat", "{}"); err == nil {
		t.Fatal("空 store 必须拒绝")
	}
}
