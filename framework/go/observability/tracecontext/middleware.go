package tracecontext

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
)

type OperationKind string

const (
	OperationWails         OperationKind = "wails"
	OperationGRPC          OperationKind = "grpc"
	OperationTask          OperationKind = "task"
	OperationSQLite        OperationKind = "sqlite"
	OperationRemoteService OperationKind = "remote_service"
)

var (
	ErrInvalidSpanRequest = errors.New("trace span 请求无效")
	spanNamePattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{1,127}$`)
	safeAttrPattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
)

type SpanRequest struct {
	Kind            OperationKind
	Component       string
	Operation       string
	IncomingHeaders map[string]string
	Sampled         bool
	SafeAttributes  map[string]string
}

type Span struct {
	Kind            OperationKind
	Component       string
	Operation       string
	Trace           Context
	OutgoingHeaders map[string]string
	SafeAttributes  map[string]string
}

type observerKey struct{}

type Observer func(Span)

func WithObserver(ctx context.Context, observer Observer) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, observerKey{}, observer)
}

func StartSpan(ctx context.Context, request SpanRequest) (context.Context, Span, error) {
	if ctx == nil || !request.Kind.valid() || !spanNamePattern.MatchString(strings.TrimSpace(request.Component)) ||
		!spanNamePattern.MatchString(strings.TrimSpace(request.Operation)) {
		return nil, Span{}, ErrInvalidSpanRequest
	}
	attributes, err := normalizeSafeAttributes(request.SafeAttributes)
	if err != nil {
		return nil, Span{}, err
	}
	trace, err := spanTrace(ctx, request)
	if err != nil {
		return nil, Span{}, err
	}
	headers := make(map[string]string, 2)
	if err := trace.Inject(headers); err != nil {
		return nil, Span{}, err
	}
	next, err := WithContext(ctx, trace)
	if err != nil {
		return nil, Span{}, err
	}
	span := Span{
		Kind: request.Kind, Component: strings.TrimSpace(request.Component), Operation: strings.TrimSpace(request.Operation),
		Trace: trace, OutgoingHeaders: headers, SafeAttributes: attributes,
	}
	if observer, ok := ctx.Value(observerKey{}).(Observer); ok && observer != nil {
		observer(span)
	}
	return next, span, nil
}

func spanTrace(ctx context.Context, request SpanRequest) (Context, error) {
	if len(request.IncomingHeaders) > 0 {
		parent, err := Extract(request.IncomingHeaders)
		if err != nil {
			return Context{}, err
		}
		return ChildOf(parent)
	}
	if parent, ok := FromContext(ctx); ok {
		return ChildOf(parent)
	}
	return New(request.Sampled)
}

func (kind OperationKind) valid() bool {
	switch kind {
	case OperationWails, OperationGRPC, OperationTask, OperationSQLite, OperationRemoteService:
		return true
	default:
		return false
	}
}

func normalizeSafeAttributes(attributes map[string]string) (map[string]string, error) {
	if len(attributes) == 0 {
		return map[string]string{}, nil
	}
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, rawKey := range keys {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		value := strings.TrimSpace(attributes[rawKey])
		if !safeAttrPattern.MatchString(key) || sensitiveTraceAttribute(key) || value == "" || len(value) > 256 {
			return nil, ErrInvalidSpanRequest
		}
		result[key] = value
	}
	return result, nil
}

func sensitiveTraceAttribute(key string) bool {
	normalized := strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	for _, candidate := range []string{
		"authorization", "token", "secret", "password", "passwd", "cookie", "jwt", "webhook",
		"refresh_token", "app_secret", "client_secret", "api_key", "private_key", "open_id", "chat_id",
		"prompt", "input", "content", "message", "raw_text", "path", "url",
	} {
		if normalized == candidate || strings.HasSuffix(normalized, "_"+candidate) {
			return true
		}
	}
	return false
}
