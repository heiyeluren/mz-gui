package model

import (
	"context"
	"strings"
	"time"
)

type contextKey int

const (
	requestIDContextKey contextKey = iota
	traceIDContextKey
	requestMetaContextKey
	callerContextKey
)

func WithRequestID(ctx context.Context, id ID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(id.String()) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey, id)
}

func WithTraceID(ctx context.Context, id ID) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(id.String()) == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDContextKey, id)
}

func WithRequestMeta(ctx context.Context, meta RequestMeta) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(meta.RequestID.String()) == "" && strings.TrimSpace(meta.TraceID.String()) == "" {
		return ctx
	}
	ctx = WithRequestID(ctx, meta.RequestID)
	ctx = WithTraceID(ctx, meta.TraceID)
	ctx = WithCaller(ctx, meta.Caller)
	return context.WithValue(ctx, requestMetaContextKey, meta)
}

func WithCaller(ctx context.Context, caller Caller) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(caller.Component) == "" {
		return ctx
	}
	return context.WithValue(ctx, callerContextKey, caller)
}

func CallerFromContext(ctx context.Context) (Caller, bool) {
	if ctx == nil {
		return Caller{}, false
	}
	caller, ok := ctx.Value(callerContextKey).(Caller)
	if !ok || strings.TrimSpace(caller.Component) == "" {
		return Caller{}, false
	}
	return caller, true
}

func BindRequest(ctx context.Context, now time.Time, meta RequestMeta) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := meta.Validate(now, false); err != nil {
		return ctx, func() {}, err
	}
	ctx = WithRequestMeta(ctx, meta)
	deadline := meta.Deadline.Time()
	if deadline.IsZero() {
		return ctx, func() {}, nil
	}
	bound, cancel := context.WithDeadline(ctx, deadline)
	return bound, cancel, nil
}

func RequestMetaFromContext(ctx context.Context) (RequestMeta, bool) {
	if ctx == nil {
		return RequestMeta{}, false
	}
	meta, ok := ctx.Value(requestMetaContextKey).(RequestMeta)
	if !ok {
		return RequestMeta{}, false
	}
	return meta, true
}

func RequestIDFromContext(ctx context.Context) (ID, bool) {
	return idFromContext(ctx, requestIDContextKey)
}

func TraceIDFromContext(ctx context.Context) (ID, bool) {
	return idFromContext(ctx, traceIDContextKey)
}

func idFromContext(ctx context.Context, key contextKey) (ID, bool) {
	if ctx == nil {
		return "", false
	}
	id, ok := ctx.Value(key).(ID)
	if !ok || strings.TrimSpace(id.String()) == "" {
		return "", false
	}
	return id, true
}
