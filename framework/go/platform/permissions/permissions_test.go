package permissions

import (
	"context"
	"testing"
)

func TestSnapshotReturnsGenericPermissionStatus(t *testing.T) {
	values, err := Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{Microphone, Accessibility, ScreenRecording, Notifications}
	if len(values) != len(want) {
		t.Fatalf("权限数量=%d", len(values))
	}
	for index, id := range want {
		if values[index].ID != id || values[index].SystemStatus == "" {
			t.Fatalf("权限项 %d 无效: %#v", index, values[index])
		}
	}
}

func TestSettingsURIUsesInjectedApplicationIdentity(t *testing.T) {
	options := SettingsOptions{ApplicationID: "com.example.sample"}
	for _, id := range []string{Microphone, Accessibility, ScreenRecording, Notifications} {
		value, ok := SettingsURI(id, options)
		if !ok || value == "" {
			t.Fatalf("权限 %s 缺少设置 URI", id)
		}
	}
	if _, ok := SettingsURI("../../custom", options); ok {
		t.Fatal("未知权限不应返回设置 URI")
	}
}

func TestSnapshotRejectsNilAndCancelledContext(t *testing.T) {
	if _, err := Snapshot(nil); err == nil {
		t.Fatal("nil context 必须拒绝")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Snapshot(ctx); err == nil {
		t.Fatal("已取消上下文必须返回错误")
	}
}
