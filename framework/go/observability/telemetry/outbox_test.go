package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDurableOutboxPersistsAcrossRestartAndAck(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	outbox := newTestOutbox(t, directory, func() time.Time { return now }, 8)
	record, err := outbox.Enqueue(context.Background(), Event{
		ID: "event-durable-0001", Name: "App.Opened", Source: "desktop", SessionID: "session-1", OccurredAt: now,
		Properties: map[string]string{"view_id": "workspace", "prompt_text": "不得保存", "detail": strings.Repeat("a", 300)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Event.Name != "app.opened" || record.Event.Properties["view_id"] != "workspace" {
		t.Fatalf("持久记录未规范化: %#v", record)
	}
	if _, exists := record.Event.Properties["prompt_text"]; exists {
		t.Fatal("持久队列不得保存 prompt 文本")
	}
	reopened := newTestOutbox(t, directory, func() time.Time { return now }, 8)
	ready, err := reopened.PeekReady(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].Event.ID != "event-durable-0001" {
		t.Fatalf("重启后回读错误: %#v", ready)
	}
	if err := reopened.Ack(context.Background(), []string{"event-durable-0001"}); err != nil {
		t.Fatal(err)
	}
	count, err := reopened.Len(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("确认删除后队列长度=%d err=%v", count, err)
	}
}

func TestDurableOutboxRetryDelayAndIdempotentEnqueue(t *testing.T) {
	now := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	outbox := newTestOutbox(t, directory, func() time.Time { return now }, 8)
	event := Event{ID: "event-retry-0001", Name: "app.module.selected", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"module_id": "market"}}
	if _, err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	count, err := outbox.Len(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("重复入队必须幂等 count=%d err=%v", count, err)
	}
	if err := outbox.MarkRetry(context.Background(), []string{"event-retry-0001"}, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	ready, err := outbox.PeekReady(context.Background(), 4)
	if err != nil || len(ready) != 0 {
		t.Fatalf("重试窗口内不得取出 ready=%#v err=%v", ready, err)
	}
	now = now.Add(3 * time.Minute)
	ready, err = outbox.PeekReady(context.Background(), 4)
	if err != nil || len(ready) != 1 || ready[0].Attempts != 1 {
		t.Fatalf("重试到期后应可取出 ready=%#v err=%v", ready, err)
	}
}

func TestDurableOutboxClearRemovesReadyAndDelayedRecords(t *testing.T) {
	now := time.Date(2026, 9, 2, 11, 30, 0, 0, time.UTC)
	directory := t.TempDir()
	outbox := newTestOutbox(t, directory, func() time.Time { return now }, 8)
	events := []Event{
		{ID: "event-clear-0001", Name: "app.module.selected", Source: "desktop", OccurredAt: now, Properties: map[string]string{"module_id": "market"}},
		{ID: "event-clear-0002", Name: "app.route.changed", Source: "desktop", OccurredAt: now, Properties: map[string]string{"route_id": "workspace-chat"}},
	}
	for _, event := range events {
		if _, err := outbox.Enqueue(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := outbox.MarkRetry(context.Background(), []string{"event-clear-0002"}, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	removed, err := outbox.Clear(context.Background())
	if err != nil || removed != 2 {
		t.Fatalf("清空 outbox 结果=%d err=%v", removed, err)
	}
	count, err := outbox.Len(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("清空后队列长度=%d err=%v", count, err)
	}
}

func TestDurableOutboxRejectsCapacityAndUnsafeDirectory(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	outbox := newTestOutbox(t, directory, func() time.Time { return now }, 1)
	first := Event{ID: "event-capacity-0001", Name: "app.route.changed", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"route_id": "workspace-chat"}}
	second := Event{ID: "event-capacity-0002", Name: "app.operation.clicked", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"control_id": "open_settings"}}
	if _, err := outbox.Enqueue(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := outbox.Enqueue(context.Background(), second); !errors.Is(err, ErrOutboxFull) {
		t.Fatalf("满队列错误=%v", err)
	}
	link := filepath.Join(t.TempDir(), "outbox-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDurableOutbox(OutboxOptions{Directory: link, MaxEvents: 8, MaxEventBytes: 4096}); !errors.Is(err, ErrInvalidOutbox) {
		t.Fatalf("符号链接目录必须拒绝: %v", err)
	}
}

func newTestOutbox(t *testing.T, directory string, now func() time.Time, capacity int) *DurableOutbox {
	t.Helper()
	outbox, err := NewDurableOutbox(OutboxOptions{Directory: directory, MaxEvents: capacity, MaxEventBytes: 4096, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return outbox
}
