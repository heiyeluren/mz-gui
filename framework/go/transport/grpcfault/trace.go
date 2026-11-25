package grpcfault

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func UnaryTrace() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		operation := "grpc.unary"
		if info != nil && strings.TrimSpace(info.FullMethod) != "" {
			operation = strings.TrimPrefix(strings.TrimSpace(info.FullMethod), "/")
		}
		spanCtx, span, err := tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
			Kind:            tracecontext.OperationGRPC,
			Component:       "grpc.server",
			Operation:       operation,
			IncomingHeaders: incomingTraceHeaders(ctx),
			Sampled:         true,
		})
		if err != nil {
			spanCtx, span, err = tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
				Kind:      tracecontext.OperationGRPC,
				Component: "grpc.server",
				Operation: operation,
				Sampled:   true,
			})
		}
		if err == nil {
			ctx = spanCtx
			if value := span.OutgoingHeaders[tracecontext.TraceParentHeader]; value != "" {
				_ = grpc.SetHeader(ctx, metadata.Pairs(tracecontext.TraceParentHeader, value))
			}
		}
		return handler(ctx, request)
	}
}

func StreamTrace() grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := context.Background()
		if stream != nil && stream.Context() != nil {
			ctx = stream.Context()
		}
		operation := "grpc.stream"
		if info != nil && strings.TrimSpace(info.FullMethod) != "" {
			operation = strings.TrimPrefix(strings.TrimSpace(info.FullMethod), "/")
		}
		spanCtx, span, err := tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
			Kind:            tracecontext.OperationGRPC,
			Component:       "grpc.server",
			Operation:       operation,
			IncomingHeaders: incomingTraceHeaders(ctx),
			Sampled:         true,
		})
		if err != nil {
			spanCtx, span, err = tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
				Kind:      tracecontext.OperationGRPC,
				Component: "grpc.server",
				Operation: operation,
				Sampled:   true,
			})
		}
		if err == nil {
			ctx = spanCtx
			if value := span.OutgoingHeaders[tracecontext.TraceParentHeader]; value != "" && stream != nil {
				_ = stream.SetHeader(metadata.Pairs(tracecontext.TraceParentHeader, value))
			}
			stream = contextServerStream{ServerStream: stream, ctx: ctx}
		}
		return handler(server, stream)
	}
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream contextServerStream) Context() context.Context {
	if stream.ctx != nil {
		return stream.ctx
	}
	if stream.ServerStream != nil {
		return stream.ServerStream.Context()
	}
	return context.Background()
}

func incomingTraceHeaders(ctx context.Context) map[string]string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil
	}
	headers := map[string]string{}
	if values := md.Get(tracecontext.TraceParentHeader); len(values) > 0 && values[0] != "" {
		headers[tracecontext.TraceParentHeader] = values[0]
	}
	if values := md.Get(tracecontext.TraceStateHeader); len(values) > 0 && values[0] != "" {
		headers[tracecontext.TraceStateHeader] = values[0]
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
