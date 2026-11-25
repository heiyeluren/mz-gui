package windowstate

import (
	"context"
	"errors"
	"testing"
)

func TestBinderSavesAndRestoresRegisteredPages(t *testing.T) {
	store := NewMemoryStore(Options{})
	binder, err := NewBinder(store)
	if err != nil {
		t.Fatal(err)
	}
	saved := map[string]string{}
	if err := binder.Register(PageCodec{
		RouteID: "settings.general",
		Save:    func(string) ([]byte, error) { return []byte(`{"theme":"dark"}`), nil },
		Restore: func(_ string, payload []byte) error {
			saved["settings.general"] = string(payload)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := binder.SaveAll(context.Background(), "window-1"); err != nil {
		t.Fatal(err)
	}
	if err := binder.RestoreAll(context.Background(), "window-1"); err != nil {
		t.Fatal(err)
	}
	if saved["settings.general"] != `{"theme":"dark"}` {
		t.Fatalf("恢复 payload 错误: %#v", saved)
	}
	routes, err := binder.RestoreRoutes(context.Background(), "window-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0] != "settings.general" {
		t.Fatalf("恢复路由 = %#v", routes)
	}
}

func TestBinderSkipsMissingPagesAndRejectsDuplicates(t *testing.T) {
	store := NewMemoryStore(Options{})
	binder, err := NewBinder(store)
	if err != nil {
		t.Fatal(err)
	}
	codec := PageCodec{
		RouteID: "chat.main",
		Save:    func(string) ([]byte, error) { return []byte(`{}`), nil },
		Restore: func(string, []byte) error { return errors.New("should not restore") },
	}
	if err := binder.Register(codec); err != nil {
		t.Fatal(err)
	}
	if err := binder.Register(codec); !errors.Is(err, ErrInvalidBinder) {
		t.Fatalf("重复注册错误 = %v", err)
	}
	if err := binder.RestoreAll(context.Background(), "window-missing"); err != nil {
		t.Fatalf("缺失页面应跳过: %v", err)
	}
	if _, err := NewBinder(nil); !errors.Is(err, ErrInvalidBinder) {
		t.Fatalf("空 store 错误 = %v", err)
	}
}

func TestBinderRequireReportsMissingRoutes(t *testing.T) {
	store := NewMemoryStore(Options{})
	binder, err := NewBinder(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := binder.Register(PageCodec{
		RouteID: "workspace.chat",
		Save:    func(string) ([]byte, error) { return []byte(`{}`), nil },
		Restore: func(string, []byte) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := binder.Require("workspace.chat"); err != nil {
		t.Fatalf("已注册路由必须通过: %v", err)
	}
	if err := binder.Require("workspace.chat", "view.settings"); !errors.Is(err, ErrMissingRoute) {
		t.Fatalf("缺失路由错误 = %v", err)
	}
	if got := binder.Routes(); len(got) != 1 || got[0] != "workspace.chat" {
		t.Fatalf("已注册路由 = %#v", got)
	}
}
