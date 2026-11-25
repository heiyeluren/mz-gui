package model

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidRequestMeta = errors.New("Framework 请求元数据无效")
	componentPattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

type Caller struct {
	Component  string  `json:"component"`
	Version    Version `json:"version"`
	InstanceID ID      `json:"instance_id"`
}

type RequestMeta struct {
	RequestID      ID        `json:"request_id"`
	TraceID        ID        `json:"trace_id"`
	Deadline       Timestamp `json:"deadline"`
	IdempotencyKey ID        `json:"idempotency_key,omitempty"`
	Caller         Caller    `json:"caller"`
}

func (meta RequestMeta) Validate(now time.Time, write bool) error {
	_, requestErr := ParseID(meta.RequestID.String())
	_, traceErr := ParseID(meta.TraceID.String())
	_, instanceErr := ParseID(meta.Caller.InstanceID.String())
	_, versionErr := ParseVersion(meta.Caller.Version.String())
	deadline := meta.Deadline.Time()
	if now.IsZero() || requestErr != nil || traceErr != nil || instanceErr != nil || versionErr != nil ||
		!componentPattern.MatchString(strings.TrimSpace(meta.Caller.Component)) || deadline.IsZero() ||
		!deadline.After(now.UTC()) || deadline.After(now.UTC().Add(24*time.Hour)) {
		return ErrInvalidRequestMeta
	}
	if meta.IdempotencyKey != "" {
		if _, err := ParseID(meta.IdempotencyKey.String()); err != nil {
			return ErrInvalidRequestMeta
		}
	}
	if write && meta.IdempotencyKey == "" {
		return ErrInvalidRequestMeta
	}
	return nil
}
