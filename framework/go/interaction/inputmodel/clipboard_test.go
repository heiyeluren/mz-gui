package inputmodel

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeClipboardAcceptsPlainTextAndRejectsSecrets(t *testing.T) {
	payload, err := SanitizeClipboard(ClipboardPayload{Text: "hello\tworld\nnext"}, 64)
	if err != nil || payload.MediaType != "text/plain" || payload.Text != "hello\tworld\nnext" {
		t.Fatalf("普通文本应通过: %#v err=%v", payload, err)
	}
	if _, err := SanitizeClipboard(ClipboardPayload{Text: "token=secret-value"}, 64); !errors.Is(err, ErrInvalidClipboard) {
		t.Fatalf("敏感剪贴板必须拒绝: %v", err)
	}
	if _, err := SanitizeClipboard(ClipboardPayload{Text: "ok", MediaType: "image/png"}, 64); !errors.Is(err, ErrInvalidClipboard) {
		t.Fatalf("非文本类型必须拒绝: %v", err)
	}
	if _, err := SanitizeClipboard(ClipboardPayload{Text: strings.Repeat("a", 8)}, 4); !errors.Is(err, ErrInvalidClipboard) {
		t.Fatal("超长剪贴板必须拒绝")
	}
}
