package panicguard

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

// Produce 先用 Run 收口 panic，再经 Mapper 把 sentinel 映射为公开错误。
func Produce(ctx context.Context, mapper *fault.Mapper, options Options, operation func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	options = applyContextIdentity(ctx, options)
	err := Run(ctx, options, operation)
	if mapper != nil {
		if produced := mapper.Produce(err); produced != nil {
			return produced
		}
		return nil
	}
	return err
}

// ProduceResult 给返回 (T, error) 的 Wails/Service 方法边界使用。
func ProduceResult[T any](ctx context.Context, mapper *fault.Mapper, options Options, operation func(context.Context) (T, error)) (T, error) {
	var zero T
	var value T
	err := Produce(ctx, mapper, options, func(ctx context.Context) error {
		if operation == nil {
			return nil
		}
		var opErr error
		value, opErr = operation(ctx)
		return opErr
	})
	if err != nil {
		return zero, err
	}
	return value, nil
}

func applyContextIdentity(ctx context.Context, options Options) Options {
	if strings.TrimSpace(options.RequestID) == "" {
		if id, ok := model.RequestIDFromContext(ctx); ok {
			options.RequestID = id.String()
		}
	}
	if strings.TrimSpace(options.TraceID) == "" {
		if id, ok := model.TraceIDFromContext(ctx); ok {
			options.TraceID = id.String()
		}
	}
	if options.Health == nil {
		if marker, ok := HealthMarkerFromContext(ctx); ok {
			options.Health = marker
		}
	}
	if options.Reporter == nil {
		if reporter, ok := ReporterFromContext(ctx); ok {
			options.Reporter = reporter
		}
	}
	if caller, ok := model.CallerFromContext(ctx); ok {
		options.Caller = caller.Component
	}
	return options
}
