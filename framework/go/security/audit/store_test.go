package audit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auditmigrations "github.com/heiyeluren/mz-gui/framework/go/security/audit/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestStoreAppendsRedactedRecordsAndRejectsWhenFull(t *testing.T) {
	t.Parallel()
	store, err := NewStore(StoreOptions{MaxEvents: 1})
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Append(Event{
		ID:         "audit-store-1",
		OccurredAt: time.Date(2026, 8, 16, 14, 30, 0, 0, time.UTC),
		Actor:      Actor{Kind: ActorKindProcess, ID: "desktop"},
		Action:     "rpc.write",
		Capability: "task.write",
		Decision:   DecisionAllow,
		ReasonCode: "authorization.allowed",
		RequestID:  "request-1",
		Metadata:   map[string]any{"token": "secret-value", "path": "/tmp/ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Decision != string(DecisionAllow) || record.MetadataJSON == "" {
		t.Fatalf("审计写入记录异常: %#v", record)
	}
	if strings.Contains(record.MetadataJSON, "secret-value") {
		t.Fatalf("审计存储未脱敏: %s", record.MetadataJSON)
	}
	if _, err := store.Append(Event{
		ID:         "audit-store-2",
		OccurredAt: time.Date(2026, 8, 16, 14, 31, 0, 0, time.UTC),
		Actor:      Actor{Kind: ActorKindProcess, ID: "desktop"},
		Action:     "rpc.write",
		Decision:   DecisionDeny,
	}); !errors.Is(err, ErrStoreFull) {
		t.Fatalf("容量满必须拒绝: %v", err)
	}
	listed := store.List()
	if len(listed) != 1 || listed[0].AuditID != "audit-store-1" {
		t.Fatalf("审计列表异常: %#v", listed)
	}
}

func TestSQLiteStoreAppendsAndQueriesAuditRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := frameworksqlite.Open(ctx, frameworksqlite.Options{
		Path:    filepath.Join(t.TempDir(), "audit.db"),
		Bundles: []frameworksqlite.MigrationBundle{auditmigrations.Bundle()},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := NewSQLiteStore(database)
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	events := []Event{
		auditEvent("audit-sql-1", base, DecisionAllow, "filesystem.read", "workspace", "grant-1", map[string]any{"token": "secret-1", "safe": "a"}),
		auditEvent("audit-sql-2", base.Add(time.Minute), DecisionDeny, "filesystem.write", "workspace", "grant-2", map[string]any{"path": "/Users/black/private.txt"}),
		auditEvent("audit-sql-3", base.Add(2*time.Minute), DecisionError, "helper.elevate", "helper", "update", map[string]any{"authorization": "Bearer secret-2"}),
	}
	for _, event := range events {
		if _, err := store.Append(ctx, event); err != nil {
			t.Fatalf("写入审计事件 %s: %v", event.ID, err)
		}
	}

	first, err := store.List(ctx, Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 2 || first.Records[0].AuditID != "audit-sql-3" || first.Records[1].AuditID != "audit-sql-2" || first.NextCursor == "" {
		t.Fatalf("第一页错误: %+v", first)
	}
	for _, record := range first.Records {
		if strings.Contains(record.MetadataJSON, "secret-") || strings.Contains(record.MetadataJSON, "/Users/black") {
			t.Fatalf("SQLite 审计查询泄露敏感 metadata: %#v", record)
		}
	}
	second, err := store.List(ctx, Query{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != 1 || second.Records[0].AuditID != "audit-sql-1" || second.NextCursor != "" {
		t.Fatalf("第二页错误: %+v", second)
	}
	filtered, err := store.List(ctx, Query{Decision: string(DecisionDeny), ResourceType: "workspace", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Records) != 1 || filtered.Records[0].AuditID != "audit-sql-2" {
		t.Fatalf("过滤查询错误: %+v", filtered)
	}
	if _, err := store.List(ctx, Query{Limit: 501}); err == nil {
		t.Fatal("超过上限的审计查询必须拒绝")
	}
	if _, err := store.List(ctx, Query{Cursor: "bad-cursor"}); err == nil {
		t.Fatal("非法 cursor 必须拒绝")
	}
}

func auditEvent(id string, at time.Time, decision Decision, capability, resourceType, resourceID string, metadata map[string]any) Event {
	return Event{
		ID:         id,
		OccurredAt: at,
		Actor:      Actor{Kind: ActorKindProcess, ID: "desktop"},
		Action:     "capability.invoke",
		Capability: capability,
		Resource:   Resource{Type: resourceType, ID: resourceID},
		Decision:   decision,
		ReasonCode: "policy.checked",
		RequestID:  "request-" + id,
		Metadata:   metadata,
	}
}
