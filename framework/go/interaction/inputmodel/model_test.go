package inputmodel

import (
	"errors"
	"testing"
	"time"
)

func TestRequestMetaValidateAndPendingKey(t *testing.T) {
	meta := RequestMeta{
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		WindowID:       "win-1",
		Target:         "button",
		Source:         SourceDeepLink,
		Device:         DeviceSystem,
		Timestamp:      time.Now(),
		IdempotencyKey: "idem-1",
	}
	if err := meta.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := meta.EffectivePendingKey("settings.open"); got != "settings.open:idem-1" {
		t.Fatalf("幂等 pending key 错误: %s", got)
	}
	meta.PendingKey = "explicit"
	if got := meta.EffectivePendingKey("settings.open"); got != "explicit" {
		t.Fatalf("显式 pending key 应优先，实际为 %s", got)
	}
}

func TestRequestMetaRejectsInvalidSourceAndRequiredFields(t *testing.T) {
	meta := RequestMeta{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		WindowID:      "win-1",
		Target:        "button",
		Source:        "bad",
		Device:        DeviceSystem,
		Timestamp:     time.Now(),
	}
	if err := meta.Validate(); !errors.Is(err, ErrInvalidMeta) {
		t.Fatalf("非法来源应失败，实际为 %v", err)
	}
	meta.Source = SourceMenu
	meta.RequestID = ""
	if err := meta.Validate(); !errors.Is(err, ErrInvalidMeta) {
		t.Fatalf("缺少 request id 应失败，实际为 %v", err)
	}
}
