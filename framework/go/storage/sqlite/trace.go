package sqlite

import (
	"context"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func startSQLiteSpan(ctx context.Context, operation string) (context.Context, tracecontext.Span, error) {
	return tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
		Kind:      tracecontext.OperationSQLite,
		Component: "sqlite",
		Operation: operation,
		Sampled:   true,
	})
}
