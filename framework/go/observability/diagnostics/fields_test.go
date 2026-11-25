package diagnostics

import "testing"

func TestRedactFieldsMasksSensitiveKeysAndInlineSecrets(t *testing.T) {
	got := RedactFields(map[string]string{
		"token":   "abc",
		"request": "ok",
		"note":    "see https://example.com/a",
	})
	if got["token"] != "[REDACTED]" {
		t.Fatalf("敏感 key 必须脱敏: %#v", got)
	}
	if got["request"] != "ok" {
		t.Fatalf("普通字段应保留: %#v", got)
	}
	if got["note"] != "see [REDACTED_URL]" {
		t.Fatalf("内联 URL 必须脱敏: %#v", got)
	}
	if RedactFields(nil) != nil {
		t.Fatal("空 map 应返回 nil")
	}
}
