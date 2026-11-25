package windowstate

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreRestoresPagesAcrossWindows(t *testing.T) {
	store := NewMemoryStore(Options{})
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "chat", Revision: 2, Payload: []byte(`{"draft":"hello"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "settings", Revision: 1, Payload: []byte(`{"tab":"net"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.RestorePages(context.Background(), "win-a", "win-b"); err != nil {
		t.Fatal(err)
	}
	chat, err := store.LoadPage(context.Background(), "win-b", "chat")
	if err != nil || string(chat.Payload) != `{"draft":"hello"}` || chat.WindowID != "win-b" {
		t.Fatalf("chat 页未恢复: %#v %v", chat, err)
	}
	settings, err := store.LoadPage(context.Background(), "win-b", "settings")
	if err != nil || string(settings.Payload) != `{"tab":"net"}` {
		t.Fatalf("settings 页未恢复: %#v %v", settings, err)
	}
	pages, err := store.ListPages(context.Background(), "win-a")
	if err != nil || len(pages) != 2 {
		t.Fatalf("源窗口页面数错误: %#v %v", pages, err)
	}
}

func TestMemoryStorePageRequiresRouteAndRejectsStaleRevision(t *testing.T) {
	store := NewMemoryStore(Options{})
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "", Revision: 1, Payload: []byte("x")}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("空 route 必须拒绝: %v", err)
	}
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "chat", Revision: 3, Payload: []byte("v3")}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePage(context.Background(), Snapshot{WindowID: "win-a", RouteID: "chat", Revision: 2, Payload: []byte("v2")}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("旧页面 revision 必须拒绝: %v", err)
	}
}
