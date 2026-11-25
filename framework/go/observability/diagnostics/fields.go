package diagnostics

func RedactFields(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		if sensitiveKey(key) {
			result[key] = "[REDACTED]"
			continue
		}
		result[key] = redactText(value)
	}
	return result
}
