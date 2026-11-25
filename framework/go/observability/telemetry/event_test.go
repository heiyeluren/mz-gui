package telemetry

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRemovesSensitivePropertiesAndBoundsValues(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	event, err := Normalize(Event{ID: "event-00000001", Name: "App.Operation.Clicked", Source: "desktop",
		SessionID: "session-1", OccurredAt: now, Properties: map[string]string{
			"module_id": "market", "prompt_text": "不得保存", "file_path": "/private/data", "detail": strings.Repeat("a", 300),
		}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if event.Name != "app.operation.clicked" || event.Properties["module_id"] != "market" {
		t.Fatalf("规范化结果错误: %+v", event)
	}
	if _, exists := event.Properties["prompt_text"]; exists {
		t.Fatal("敏感 prompt 属性不应保留")
	}
	if _, exists := event.Properties["file_path"]; exists {
		t.Fatal("路径属性不应保留")
	}
	if len(event.Properties["detail"]) != MaxPropertyValue {
		t.Fatalf("属性值长度=%d", len(event.Properties["detail"]))
	}
}

func TestNormalizeRejectsInvalidIdentityAndTimestamp(t *testing.T) {
	now := time.Now().UTC()
	for _, event := range []Event{
		{ID: "short", Name: "app.opened", Source: "desktop", OccurredAt: now},
		{ID: "event-00000001", Name: "opened", Source: "desktop", OccurredAt: now},
		{ID: "event-00000001", Name: "app.opened", Source: "Desktop UI", OccurredAt: now},
		{ID: "event-00000001", Name: "app.opened", Source: "desktop", OccurredAt: now.Add(-25 * time.Hour)},
	} {
		if _, err := Normalize(event, now); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("事件 %+v 错误=%v", event, err)
		}
	}
}

func TestNormalizeLimitsPropertiesDeterministically(t *testing.T) {
	now := time.Now().UTC()
	properties := map[string]string{}
	for index := 0; index < 40; index++ {
		properties[fmt.Sprintf("field_a%02d", index)] = "value"
	}
	event, err := Normalize(Event{ID: "event-00000001", Name: "app.opened", Source: "desktop", OccurredAt: now, Properties: properties}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Properties) != MaxProperties {
		t.Fatalf("属性数量=%d", len(event.Properties))
	}
}
