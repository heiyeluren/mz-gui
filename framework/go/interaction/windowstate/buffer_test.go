package windowstate

import (
	"context"
	"testing"
)

func TestBufferCodecRoundTripsPayload(t *testing.T) {
	store := NewMemoryStore(Options{})
	binder, err := NewBinder(store)
	if err != nil {
		t.Fatal(err)
	}
	buffer := &Buffer{}
	if err := binder.Register(buffer.Codec("workspace.chat")); err != nil {
		t.Fatal(err)
	}
	buffer.Put("window-1", "workspace.chat", []byte(`{"draft":"hello"}`))
	if err := binder.SaveAll(context.Background(), "window-1"); err != nil {
		t.Fatal(err)
	}
	buffer.Put("window-1", "workspace.chat", []byte(`{"draft":"stale"}`))
	if err := binder.RestoreAll(context.Background(), "window-1"); err != nil {
		t.Fatal(err)
	}
	if got := string(buffer.Get("window-1", "workspace.chat")); got != `{"draft":"hello"}` {
		t.Fatalf("恢复 payload 错误: %s", got)
	}
}

func TestBufferGetDefaultsToEmptyObject(t *testing.T) {
	buffer := &Buffer{}
	if got := string(buffer.Get("window-1", "missing")); got != "{}" {
		t.Fatalf("缺失页面默认 payload 错误: %s", got)
	}
}
