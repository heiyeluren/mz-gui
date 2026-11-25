package wailsapp

import (
	"context"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
)

func TestPersistAndRestorePagesAcrossWindows(t *testing.T) {
	store := windowstate.NewMemoryStore(windowstate.Options{})
	if err := PersistPageState(context.Background(), store, "win-main", "chat", `{"draft":"hello"}`); err != nil {
		t.Fatal(err)
	}
	if err := PersistPageState(context.Background(), store, "win-main", "settings", `{"tab":"net"}`); err != nil {
		t.Fatal(err)
	}
	if err := RestoreWindowPages(context.Background(), store, "win-main", "win-aux"); err != nil {
		t.Fatal(err)
	}
	chat, err := store.LoadPage(context.Background(), "win-aux", "chat")
	if err != nil || string(chat.Payload) != `{"draft":"hello"}` {
		t.Fatalf("辅助窗口未恢复 chat: %#v %v", chat, err)
	}
	settings, err := store.LoadPage(context.Background(), "win-aux", "settings")
	if err != nil || string(settings.Payload) != `{"tab":"net"}` {
		t.Fatalf("辅助窗口未恢复 settings: %#v %v", settings, err)
	}
}

func TestPersistPageStateRejectsInvalidInput(t *testing.T) {
	if err := PersistPageState(context.Background(), nil, "win", "chat", "{}"); err == nil {
		t.Fatal("空 store 必须拒绝")
	}
	if err := RestoreWindowPages(context.Background(), windowstate.NewMemoryStore(windowstate.Options{}), "", "win-b"); err == nil {
		t.Fatal("空源窗口必须拒绝")
	}
}
