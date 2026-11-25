package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func FingerprintCrash(errorClass, component, stack string) (string, error) {
	errorClass = strings.TrimSpace(errorClass)
	component = strings.TrimSpace(component)
	if errorClass == "" || component == "" {
		return "", ErrInvalidCrashSpool
	}
	payload := errorClass + "\n" + component + "\n" + strings.TrimSpace(stack)
	sum := sha256.Sum256([]byte(payload))
	return "c" + hex.EncodeToString(sum[:16]), nil
}
