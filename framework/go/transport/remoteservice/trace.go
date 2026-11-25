package remoteservice

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

type remoteSpanKey struct{}

func startRemoteSpan(ctx context.Context, request Request) (context.Context, tracecontext.Span, error) {
	operation := strings.TrimSpace(request.Method)
	if operation == "" {
		operation = "HTTP"
	}
	incoming := map[string]string{}
	if request.Headers != nil {
		if value := request.Headers.Get(tracecontext.TraceParentHeader); value != "" {
			incoming[tracecontext.TraceParentHeader] = value
		}
		if value := request.Headers.Get(tracecontext.TraceStateHeader); value != "" {
			incoming[tracecontext.TraceStateHeader] = value
		}
	}
	return tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
		Kind:            tracecontext.OperationRemoteService,
		Component:       "remoteservice.client",
		Operation:       operation,
		IncomingHeaders: incoming,
		Sampled:         true,
	})
}

func withRemoteSpan(ctx context.Context, span tracecontext.Span) context.Context {
	return context.WithValue(ctx, remoteSpanKey{}, span)
}

func remoteSpanFrom(ctx context.Context) (tracecontext.Span, bool) {
	span, ok := ctx.Value(remoteSpanKey{}).(tracecontext.Span)
	return span, ok
}
