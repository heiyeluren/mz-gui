package messagebus

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/adapters/contract"
)

func TestDurableBusPersistsMessagesAcrossRestartWithSameContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.db")
	first, err := NewDurable(DurableOptions{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Publish(context.Background(), mustMessage(t, "runtime.events", "art-1", "idem-1")); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	second, err := NewDurable(DurableOptions{DatabasePath: path})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	stream, err := second.Subscribe(context.Background(), contract.SubscriptionRequest{Meta: readMeta(t, "sub-durable"), Topic: "runtime.events"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close(context.Background())
	if got := mustNext(t, stream); got.Message.PayloadRef.ID != "art-1" {
		t.Fatalf("重启后消息丢失: %#v", got)
	}
	if _, err := second.Publish(context.Background(), mustMessage(t, "runtime.events", "art-2", "idem-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Publish(context.Background(), mustMessage(t, "runtime.events", "art-1", "idem-1")); err != nil {
		t.Fatal(err)
	}
	if got := mustNext(t, stream); got.Message.PayloadRef.ID != "art-2" {
		t.Fatalf("幂等或顺序错误: %#v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := stream.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("空游标后的第三条消息不应存在: %v", err)
	}
}

func TestDurableBusRejectsEmptyDatabasePath(t *testing.T) {
	if _, err := NewDurable(DurableOptions{}); err == nil {
		t.Fatal("空数据库路径必须拒绝")
	}
}
