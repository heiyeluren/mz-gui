package configfile

import (
	"errors"
	"strings"
	"testing"
)

func TestRegistryValidatesRegisteredSchemas(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Schema{
		ID: "app.settings", Version: 1,
		Validate: func(data []byte) error {
			if !strings.Contains(string(data), "schema_version = 1") {
				return ErrInvalidSchema
			}
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate("app.settings", 1, []byte("schema_version = 1\n")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate("app.settings", 1, []byte("schema_version = 2\n")); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("validator 错误 = %v", err)
	}
	if err := registry.Register(Schema{ID: "app.settings", Version: 1, Validate: func([]byte) error { return nil }}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("重复 schema 错误 = %v", err)
	}
	if err := registry.Validate("missing", 1, nil); !errors.Is(err, ErrSchemaNotFound) {
		t.Fatalf("缺失 schema 错误 = %v", err)
	}
}

func TestEnforceTOMLLockedKeys(t *testing.T) {
	before := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"light\"\n")
	after := []byte("schema_version = 1\n\n[security]\nmode = \"strict\"\n\n[ui]\ntheme = \"dark\"\n")
	if err := EnforceTOMLLockedKeys(before, after, []string{"security.mode"}); err != nil {
		t.Fatal(err)
	}
	changed := []byte("schema_version = 1\n\n[security]\nmode = \"loose\"\n")
	if err := EnforceTOMLLockedKeys(before, changed, []string{"security.mode"}); !errors.Is(err, ErrPolicyLocked) {
		t.Fatalf("锁定字段错误 = %v", err)
	}
}

func TestHubPublishesSnapshotsAndSupportsCancel(t *testing.T) {
	hub := NewHub()
	subscription := hub.Subscribe("settings", 1)
	snapshot := snapshot([]byte("schema_version = 1\n"))
	hub.Publish(snapshot)
	event := <-subscription.C
	if event.Component != "settings" || event.Snapshot.Revision != snapshot.Revision || string(event.Snapshot.Data) != string(snapshot.Data) {
		t.Fatalf("事件异常: %#v", event)
	}
	subscription.Cancel()
	if _, ok := <-subscription.C; ok {
		t.Fatal("取消订阅后 channel 应关闭")
	}
}
