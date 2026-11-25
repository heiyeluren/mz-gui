package wailsapp

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func StartCommandSpan(ctx context.Context, operation string) (context.Context, tracecontext.Span, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "wails.command"
	}
	return tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
		Kind:      tracecontext.OperationWails,
		Component: "wails.command",
		Operation: operation,
		Sampled:   true,
	})
}
