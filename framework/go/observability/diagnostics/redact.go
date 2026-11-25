package diagnostics

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	assignmentPattern  = regexp.MustCompile(`(?i)\b(authorization|token|secret|password|passwd|cookie|jwt|webhook|refresh[_-]?token|app[_-]?secret|client[_-]?secret|api[_-]?key|private[_-]?key|open[_-]?id|chat[_-]?id|prompt|input|content|message[_-]?history|raw[_-]?text|path|url|device[_-]?id|serial[_-]?number)\b(\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,}"']+)`)
	bearerPattern      = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]+`)
	urlPattern         = regexp.MustCompile(`(?i)\bhttps?://[^\s"']+`)
	unixPathPattern    = regexp.MustCompile(`(?:/Users|/home|/private|/var|/tmp)/[^\s"']+`)
	windowsPathPattern = regexp.MustCompile(`[A-Za-z]:\\[^\r\n"']+`)
)

// Redact 对日志文本执行独立于日志器的二次脱敏，防止历史日志和自由文本泄露。
// W115-G684：整份 JSON 先按树脱敏，避免 pretty JSON 被按行替换时破坏引号。
func Redact(data []byte) []byte {
	data = bytes.ToValidUTF8(data, []byte("?"))
	if len(bytes.TrimSpace(data)) == 0 {
		return data
	}
	var document any
	if json.Unmarshal(data, &document) == nil {
		return marshalRedacted(document)
	}
	lines := bytes.Split(data, []byte("\n"))
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var value any
		if json.Unmarshal(line, &value) == nil {
			lines[index] = marshalRedacted(value)
			continue
		}
		lines[index] = []byte(redactText(string(line)))
	}
	return bytes.Join(lines, []byte("\n"))
}

func marshalRedacted(value any) []byte {
	clean := redactValue("", value)
	data, err := json.Marshal(clean)
	if err != nil {
		return []byte("{\"message\":\"[REDACTED]\"}")
	}
	return data
}

func redactValue(key string, value any) any {
	if sensitiveKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, child := range typed {
			result[childKey] = redactValue(childKey, child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactValue(key, child)
		}
		return result
	case string:
		return redactText(typed)
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	for _, candidate := range []string{
		"authorization", "token", "secret", "password", "passwd", "cookie", "jwt", "webhook",
		"api_key", "private_key", "open_id", "chat_id", "prompt", "input", "content", "message_history",
		"raw_text", "path", "url", "device_id", "serial_number",
	} {
		if normalized == candidate || strings.HasSuffix(normalized, "_"+candidate) {
			return true
		}
	}
	return false
}

func redactText(value string) string {
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	value = assignmentPattern.ReplaceAllString(value, "$1$2[REDACTED]")
	value = urlPattern.ReplaceAllString(value, "[REDACTED_URL]")
	value = unixPathPattern.ReplaceAllString(value, "[REDACTED_PATH]")
	return windowsPathPattern.ReplaceAllString(value, "[REDACTED_PATH]")
}
