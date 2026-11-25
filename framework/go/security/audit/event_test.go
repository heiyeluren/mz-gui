package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventValidateAndRecord(t *testing.T) {
	t.Parallel()
	event := Event{
		ID:         "audit-001",
		OccurredAt: time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC),
		Actor:      Actor{Kind: ActorKindProcess, ID: "desktop"},
		Action:     "capability.invoke",
		Capability: "transport.local",
		Resource:   Resource{Type: "ipc", ID: "agentd"},
		Decision:   DecisionAllow,
		ReasonCode: "policy.allowed",
		RequestID:  "req-001",
		Metadata: map[string]any{
			"trace_id": "trace-001",
			"token":    "secret-value",
		},
	}

	if err := event.Validate(); err != nil {
		t.Fatalf("合法审计事件校验失败: %v", err)
	}
	record, err := event.Record()
	if err != nil {
		t.Fatalf("转换审计记录: %v", err)
	}
	if record.AuditID != event.ID || record.Actor != "process:desktop" || record.Decision != string(DecisionAllow) {
		t.Fatalf("审计记录字段异常: %#v", record)
	}
	if strings.Contains(record.MetadataJSON, "secret-value") {
		t.Fatalf("审计 metadata 未脱敏: %s", record.MetadataJSON)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(record.MetadataJSON), &metadata); err != nil {
		t.Fatalf("metadata JSON 无效: %v", err)
	}
	if metadata["token"] != RedactedValue {
		t.Fatalf("敏感字段脱敏异常: %#v", metadata)
	}
}

func TestEventValidateRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	base := Event{
		ID:         "audit-001",
		OccurredAt: time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC),
		Actor:      Actor{Kind: ActorKindUser, ID: "user-001"},
		Action:     "settings.update",
		Decision:   DecisionDeny,
	}

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{name: "缺少 ID", mutate: func(event *Event) { event.ID = "" }},
		{name: "缺少时间", mutate: func(event *Event) { event.OccurredAt = time.Time{} }},
		{name: "缺少 actor", mutate: func(event *Event) { event.Actor.ID = "" }},
		{name: "缺少 action", mutate: func(event *Event) { event.Action = "" }},
		{name: "非法 decision", mutate: func(event *Event) { event.Decision = "maybe" }},
		{name: "非法 capability", mutate: func(event *Event) { event.Capability = "Transport Local" }},
		{name: "非法 reason_code", mutate: func(event *Event) { event.ReasonCode = "not allowed" }},
		{name: "非法 request_id", mutate: func(event *Event) { event.RequestID = "req id" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := base
			test.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("非法审计事件未被拒绝")
			}
		})
	}
}

func TestRedactMetadataRecursesNestedValues(t *testing.T) {
	t.Parallel()
	metadata := RedactMetadata(map[string]any{
		"authorization": "Bearer abc",
		"nested": map[string]any{
			"api_secret": "secret",
			"safe":       "value",
		},
		"items": []any{
			map[string]any{"refresh_token": "token"},
		},
	})

	if metadata["authorization"] != RedactedValue {
		t.Fatalf("authorization 未脱敏: %#v", metadata)
	}
	nested := metadata["nested"].(map[string]any)
	if nested["api_secret"] != RedactedValue || nested["safe"] != "value" {
		t.Fatalf("嵌套字段脱敏异常: %#v", nested)
	}
	items := metadata["items"].([]any)
	item := items[0].(map[string]any)
	if item["refresh_token"] != RedactedValue {
		t.Fatalf("数组内字段未脱敏: %#v", item)
	}
}

func TestRedactMetadataCoversKeyAliasesUsedByLeakscan(t *testing.T) {
	t.Parallel()
	metadata := RedactMetadata(map[string]any{
		"api_key":    "key-1",
		"api-key":    "key-2",
		"APIKEY":     "key-3",
		"passwd":     "pw",
		"credential": "cred",
		"path":       "/Users/black/private.txt",
		"url":        "https://secret.example.test",
		"safe":       "value",
	})
	for _, key := range []string{"api_key", "api-key", "APIKEY", "passwd", "credential", "path", "url"} {
		if metadata[key] != RedactedValue {
			t.Fatalf("%s 未脱敏: %#v", key, metadata)
		}
	}
	if metadata["safe"] != "value" {
		t.Fatalf("非敏感字段被误伤: %#v", metadata)
	}
}
