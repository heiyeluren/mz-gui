package inputmodel

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSanitizeDragDropAcceptsInternalTextAndRejectsUnsafePayloads(t *testing.T) {
	meta := RequestMeta{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		WindowID:      "win-1",
		Target:        "editor",
		Source:        SourceMouse,
		Device:        DeviceMouse,
		Timestamp:     time.Now(),
	}
	payload, err := SanitizeDragDrop(DragDropPayload{
		Meta:      meta,
		MediaType: "text/plain",
		Text:      "hello\tworld",
		Origin:    OriginInternalWindow,
	}, 64)
	if err != nil || payload.Text != "hello\tworld" || payload.Origin != OriginInternalWindow {
		t.Fatalf("内部窗口文本应通过: %#v err=%v", payload, err)
	}

	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "text/plain", Text: "token=secret-value", Origin: OriginExternal,
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatalf("敏感拖放必须拒绝: %v", err)
	}
	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "image/png", Text: "ok", Origin: OriginInternalWindow,
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatalf("非文本类型必须拒绝: %v", err)
	}
	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "text/plain", Text: strings.Repeat("a", 8), Origin: OriginUnknown,
	}, 4); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatal("超长拖放必须拒绝")
	}
	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "text/uri-list", Text: "javascript:alert(1)", Origin: OriginExternal,
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatal("危险 URI 必须拒绝")
	}
	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "text/uri-list", Text: "file:///tmp/payload.exe", Origin: OriginExternal,
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatal("可执行文件 URI 必须拒绝")
	}
	accepted, err := SanitizeDragDrop(DragDropPayload{
		Meta:      meta,
		MediaType: "text/uri-list",
		Text:      "https://example.test/doc.md",
		Origin:    OriginExternal,
	}, 64)
	if err != nil || accepted.Text != "https://example.test/doc.md" {
		t.Fatalf("https URI 应通过: %#v err=%v", accepted, err)
	}
}

func TestSanitizeDragDropRejectsInvalidMetaAndOrigin(t *testing.T) {
	if _, err := SanitizeDragDrop(DragDropPayload{
		MediaType: "text/plain",
		Text:      "ok",
		Origin:    OriginInternalWindow,
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatalf("无效元数据必须拒绝: %v", err)
	}
	meta := RequestMeta{
		RequestID:     "req-1",
		CorrelationID: "corr-1",
		WindowID:      "win-1",
		Target:        "editor",
		Source:        SourceMouse,
		Device:        DeviceMouse,
		Timestamp:     time.Now(),
	}
	if _, err := SanitizeDragDrop(DragDropPayload{
		Meta: meta, MediaType: "text/plain", Text: "ok", Origin: "clipboard",
	}, 64); !errors.Is(err, ErrInvalidDragDrop) {
		t.Fatalf("非法来源必须拒绝: %v", err)
	}
}
