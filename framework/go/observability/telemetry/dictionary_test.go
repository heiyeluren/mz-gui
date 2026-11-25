package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDictionaryRejectsInvalidDocuments(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	valid := mustJSON(t, map[string]any{
		"schema_version": DictionarySchemaVersion,
		"events": []map[string]any{{
			"name": "sample.widget.clicked", "privacy_level": "operational",
			"required_properties": []string{"control_id"}, "allowed_properties": []string{"control_id", "target_size"},
		}},
	})
	if _, err := ParseDictionary(append(append([]byte{}, valid...), []byte(`{"extra":true}`)...)); !errors.Is(err, ErrInvalidDictionary) {
		t.Fatalf("尾随 JSON 错误=%v", err)
	}
	for _, raw := range [][]byte{
		nil,
		[]byte(`{"schema_version":1}`),
		[]byte(`{"schema_version":2,"events":[{"name":"sample.widget.clicked","privacy_level":"operational","required_properties":[],"allowed_properties":["control_id"]}]}`),
		[]byte(`{"schema_version":1,"events":[]}`),
		[]byte(`{"schema_version":1,"events":[{"name":"clicked","privacy_level":"operational","required_properties":[],"allowed_properties":[]}]}`),
		[]byte(`{"schema_version":1,"events":[{"name":"sample.widget.clicked","privacy_level":"secret","required_properties":[],"allowed_properties":[]}]}`),
		[]byte(`{"schema_version":1,"events":[{"name":"sample.widget.clicked","privacy_level":"operational","required_properties":["control_id"],"allowed_properties":["surface"]}]}`),
		[]byte(`{"schema_version":1,"events":[{"name":"sample.widget.clicked","privacy_level":"operational","required_properties":[],"allowed_properties":["prompt_text"]}]}`),
		[]byte(`{"schema_version":1,"unknown":true,"events":[{"name":"sample.widget.clicked","privacy_level":"operational","required_properties":[],"allowed_properties":[]}]}`),
		mustJSON(t, map[string]any{"schema_version": 1, "events": []map[string]any{
			{"name": "sample.widget.clicked", "privacy_level": "operational", "required_properties": []string{}, "allowed_properties": []string{"control_id"}},
			{"name": "sample.widget.clicked", "privacy_level": "sensitive", "required_properties": []string{}, "allowed_properties": []string{"control_id"}},
		}}),
	} {
		if _, err := ParseDictionary(raw); !errors.Is(err, ErrInvalidDictionary) {
			t.Fatalf("无效字典 %s 错误=%v", raw, err)
		}
	}
	dictionary, err := ParseDictionary(valid)
	if err != nil {
		t.Fatal(err)
	}
	event, err := Normalize(Event{ID: "event-00000001", Name: "sample.widget.clicked", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"control_id": "font_size"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dictionary.Apply(event); err != nil {
		t.Fatal(err)
	}
}

func TestDictionaryApplyRejectsUnknownNamesAndFiltersKeys(t *testing.T) {
	now := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	dictionary, err := ParseDictionary(mustJSON(t, map[string]any{
		"$schema":        "event-dictionary.schema.json",
		"schema_version": DictionarySchemaVersion,
		"events": []map[string]any{
			{"name": "sample.session.started", "privacy_level": "operational", "required_properties": []string{"surface"}, "allowed_properties": []string{"surface"}},
			{"name": "sample.widget.clicked", "privacy_level": "internal", "required_properties": []string{"control_id"}, "allowed_properties": []string{"control_id", "target_size"}},
		},
	}))
	if err == nil {
		t.Fatal("internal 不是合法隐私级别")
	}
	dictionary, err = ParseDictionary(mustJSON(t, map[string]any{
		"schema_version": DictionarySchemaVersion,
		"events": []map[string]any{
			{"name": "sample.session.started", "privacy_level": "operational", "required_properties": []string{"surface"}, "allowed_properties": []string{"surface"}},
			{"name": "sample.widget.clicked", "privacy_level": "operational", "required_properties": []string{"control_id"}, "allowed_properties": []string{"control_id", "target_size"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := dictionary.Lookup("sample.session.started"); !exists {
		t.Fatal("已登记事件应可查找")
	}
	if names := dictionary.Names(); len(names) != 2 || names[0] != "sample.session.started" || names[1] != "sample.widget.clicked" {
		t.Fatalf("事件名清单=%v", names)
	}
	unknown, err := Normalize(Event{ID: "event-00000001", Name: "sample.unknown.exploded", Source: "desktop", OccurredAt: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := dictionary.Apply(unknown); !errors.Is(applyErr, ErrUnknownEvent) {
		t.Fatalf("未登记事件错误=%v", applyErr)
	}
	missing, err := Normalize(Event{ID: "event-00000002", Name: "sample.widget.clicked", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"target_size": "large"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, applyErr := dictionary.Apply(missing); !errors.Is(applyErr, ErrEventProperties) {
		t.Fatalf("缺少必填属性错误=%v", applyErr)
	}
	filtered, err := Normalize(Event{ID: "event-00000003", Name: "sample.widget.clicked", Source: "desktop", OccurredAt: now,
		Properties: map[string]string{"control_id": "open_panel", "extra_flag": "1", "prompt_text": "不得保留"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := dictionary.Apply(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Properties["control_id"] != "open_panel" || len(applied.Properties) != 1 {
		t.Fatalf("允许属性过滤错误: %+v", applied.Properties)
	}
	definition, _ := dictionary.Lookup("sample.widget.clicked")
	if definition.PrivacyLevel != PrivacyOperational {
		t.Fatalf("隐私级别=%s", definition.PrivacyLevel)
	}
}

func TestZeroDictionaryRejectsAllEvents(t *testing.T) {
	now := time.Now().UTC()
	event, err := Normalize(Event{ID: "event-00000001", Name: "sample.widget.clicked", Source: "desktop", OccurredAt: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Dictionary{}).Apply(event); !errors.Is(err, ErrUnknownEvent) {
		t.Fatalf("空字典错误=%v", err)
	}
}

func TestDictionarySchemaIsEmbeddedAndProductAgnostic(t *testing.T) {
	schema := DictionarySchemaJSON()
	if !json.Valid(schema) || !bytes.Contains(schema, []byte(`"privacy_level"`)) || !bytes.Contains(schema, []byte(`"allowed_properties"`)) {
		t.Fatalf("schema 不完整: %s", schema)
	}
	var document map[string]any
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatal(err)
	}
	if document["$id"] != "https://mz-gui.local/schema/telemetry-event-dictionary-v1.json" {
		t.Fatalf("schema $id=%v", document["$id"])
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dictionary.go", "event-dictionary.schema.json"} {
		content, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		lower := strings.ToLower(string(content))
		for _, forbidden := range []string{"kongming", "工作台", "墨识", "open_settings", "font_size", "workspace"} {
			if strings.Contains(lower, strings.ToLower(forbidden)) {
				t.Errorf("%s 不得写死 Mozi 页面或控件名 %q", name, forbidden)
			}
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
