package basicconsumer_test

import (
	"context"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
	"github.com/heiyeluren/mz-gui/framework/go/interaction/eventbus"
	"github.com/heiyeluren/mz-gui/framework/go/transport/local"
)

func TestPublicContracts(t *testing.T) {
	version, err := model.ParseVersion("1.0.0")
	if err != nil || version.String() != "1.0.0" {
		t.Fatalf("版本契约异常: %v %v", version, err)
	}
	bus, err := eventbus.New[string](eventbus.Options{Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Cancel()
	if err := bus.Publish(context.Background(), "ready"); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-subscription.Events():
		if value != "ready" {
			t.Fatalf("事件内容异常: %q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("事件超时")
	}
	if err := (local.Endpoint{Name: "consumer-agent", RunDir: t.TempDir()}).Validate(); err != nil {
		t.Fatal(err)
	}
}
