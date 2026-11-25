package tracecontext

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	TraceParentHeader = "traceparent"
	TraceStateHeader  = "tracestate"
)

var (
	ErrInvalidTraceContext = errors.New("trace context 无效")
	hex32Pattern           = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex16Pattern           = regexp.MustCompile(`^[0-9a-f]{16}$`)
	hex2Pattern            = regexp.MustCompile(`^[0-9a-f]{2}$`)
)

type Context struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Sampled      bool
	TraceState   string
}

type contextKey struct{}

func New(sampled bool) (Context, error) {
	traceID, err := randomHex(16)
	if err != nil {
		return Context{}, err
	}
	spanID, err := randomHex(8)
	if err != nil {
		return Context{}, err
	}
	return Context{TraceID: traceID, SpanID: spanID, Sampled: sampled}, nil
}

func ChildOf(parent Context) (Context, error) {
	if err := parent.Validate(); err != nil {
		return Context{}, err
	}
	spanID, err := randomHex(8)
	if err != nil {
		return Context{}, err
	}
	return Context{
		TraceID:      parent.TraceID,
		SpanID:       spanID,
		ParentSpanID: parent.SpanID,
		Sampled:      parent.Sampled,
		TraceState:   parent.TraceState,
	}, nil
}

func ParseTraceParent(value string) (Context, error) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || parts[0] != "00" {
		return Context{}, ErrInvalidTraceContext
	}
	traceID := strings.ToLower(parts[1])
	spanID := strings.ToLower(parts[2])
	flags := strings.ToLower(parts[3])
	if !hex32Pattern.MatchString(traceID) || !hex16Pattern.MatchString(spanID) || !hex2Pattern.MatchString(flags) {
		return Context{}, ErrInvalidTraceContext
	}
	if isZero(traceID) || isZero(spanID) {
		return Context{}, ErrInvalidTraceContext
	}
	return Context{TraceID: traceID, SpanID: spanID, Sampled: flags[len(flags)-1] == '1'}, nil
}

func (trace Context) Validate() error {
	trace.TraceID = strings.ToLower(strings.TrimSpace(trace.TraceID))
	trace.SpanID = strings.ToLower(strings.TrimSpace(trace.SpanID))
	if !hex32Pattern.MatchString(trace.TraceID) || !hex16Pattern.MatchString(trace.SpanID) {
		return ErrInvalidTraceContext
	}
	if isZero(trace.TraceID) || isZero(trace.SpanID) {
		return ErrInvalidTraceContext
	}
	if trace.ParentSpanID != "" {
		parent := strings.ToLower(strings.TrimSpace(trace.ParentSpanID))
		if !hex16Pattern.MatchString(parent) || isZero(parent) {
			return ErrInvalidTraceContext
		}
	}
	return nil
}

func (trace Context) TraceParent() (string, error) {
	trace.TraceID = strings.ToLower(strings.TrimSpace(trace.TraceID))
	trace.SpanID = strings.ToLower(strings.TrimSpace(trace.SpanID))
	if err := trace.Validate(); err != nil {
		return "", err
	}
	flags := "00"
	if trace.Sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", trace.TraceID, trace.SpanID, flags), nil
}

func (trace Context) Inject(headers map[string]string) error {
	if headers == nil {
		return ErrInvalidTraceContext
	}
	parent, err := trace.TraceParent()
	if err != nil {
		return err
	}
	headers[TraceParentHeader] = parent
	if strings.TrimSpace(trace.TraceState) != "" {
		headers[TraceStateHeader] = strings.TrimSpace(trace.TraceState)
	}
	return nil
}

func Extract(headers map[string]string) (Context, error) {
	if headers == nil {
		return Context{}, ErrInvalidTraceContext
	}
	value := headerValue(headers, TraceParentHeader)
	trace, err := ParseTraceParent(value)
	if err != nil {
		return Context{}, err
	}
	trace.TraceState = headerValue(headers, TraceStateHeader)
	return trace, nil
}

func WithContext(ctx context.Context, trace Context) (context.Context, error) {
	if ctx == nil {
		return nil, ErrInvalidTraceContext
	}
	if err := trace.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, contextKey{}, trace), nil
}

func FromContext(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	trace, ok := ctx.Value(contextKey{}).(Context)
	return trace, ok
}

func randomHex(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	for {
		if _, err := rand.Read(buffer); err != nil {
			return "", err
		}
		value := hex.EncodeToString(buffer)
		if !isZero(value) {
			return value, nil
		}
	}
}

func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isZero(value string) bool {
	for _, char := range value {
		if char != '0' {
			return false
		}
	}
	return value != ""
}
