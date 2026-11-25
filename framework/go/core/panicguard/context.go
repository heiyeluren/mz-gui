package panicguard

import "context"

type contextKey int

const (
	healthContextKey contextKey = iota
	reporterContextKey
)

func WithHealthMarker(ctx context.Context, marker HealthMarker) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if marker == nil {
		return ctx
	}
	return context.WithValue(ctx, healthContextKey, marker)
}

func HealthMarkerFromContext(ctx context.Context) (HealthMarker, bool) {
	if ctx == nil {
		return nil, false
	}
	marker, ok := ctx.Value(healthContextKey).(HealthMarker)
	if !ok || marker == nil {
		return nil, false
	}
	return marker, true
}

func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterContextKey, reporter)
}

func ReporterFromContext(ctx context.Context) (Reporter, bool) {
	if ctx == nil {
		return nil, false
	}
	reporter, ok := ctx.Value(reporterContextKey).(Reporter)
	if !ok || reporter == nil {
		return nil, false
	}
	return reporter, true
}
