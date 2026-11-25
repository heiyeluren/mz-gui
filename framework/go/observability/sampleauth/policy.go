package sampleauth

import (
	"errors"
	"strings"
)

var (
	ErrInvalidPolicy = errors.New("观测采样策略无效")
	ErrUploadDenied  = errors.New("观测上传未授权")
	ErrInvalidUpload = errors.New("观测上传类型无效")
)

type UploadKind string

const (
	UploadTraces      UploadKind = "traces"
	UploadCrashes     UploadKind = "crashes"
	UploadDiagnostics UploadKind = "diagnostics"
)

type Policy struct {
	Rate uint8
}

func (policy Policy) Validate() error {
	if policy.Rate > 100 {
		return ErrInvalidPolicy
	}
	return nil
}

func (policy Policy) Sampled(traceID string) bool {
	if policy.Validate() != nil {
		return false
	}
	if policy.Rate == 0 {
		return false
	}
	if policy.Rate >= 100 {
		return true
	}
	return bucket(traceID) < int(policy.Rate)
}

func AuthorizeUpload(kind UploadKind, capabilities []string) error {
	required, ok := requiredCapability(kind)
	if !ok {
		return ErrInvalidUpload
	}
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == required {
			return nil
		}
	}
	return ErrUploadDenied
}

func requiredCapability(kind UploadKind) (string, bool) {
	switch kind {
	case UploadTraces:
		return "observability.upload.traces", true
	case UploadCrashes:
		return "observability.upload.crashes", true
	case UploadDiagnostics:
		return "observability.upload.diagnostics", true
	default:
		return "", false
	}
}

func bucket(traceID string) int {
	value := 0
	for _, char := range strings.ToLower(strings.TrimSpace(traceID)) {
		switch {
		case char >= '0' && char <= '9':
			value = (value*16 + int(char-'0')) % 100
		case char >= 'a' && char <= 'f':
			value = (value*16 + int(char-'a'+10)) % 100
		}
	}
	return value
}
