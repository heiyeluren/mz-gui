package grpcfault

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
	"google.golang.org/grpc"
)

func UnaryServerBoundary(options panicguard.Options) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (response any, err error) {
		boundary := strings.TrimSpace(options.Boundary)
		if info != nil && strings.TrimSpace(info.FullMethod) != "" {
			boundary += ":" + info.FullMethod
		}
		callOptions := options
		callOptions.Boundary = boundary
		err = panicguard.Run(ctx, callOptions, func(callContext context.Context) error {
			var handlerErr error
			response, handlerErr = handler(callContext, request)
			return handlerErr
		})
		if err != nil {
			return nil, ToStatus(err)
		}
		return response, nil
	}
}

func StreamServerBoundary(options panicguard.Options) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := context.Background()
		if stream != nil && stream.Context() != nil {
			ctx = stream.Context()
		}
		boundary := strings.TrimSpace(options.Boundary)
		if info != nil && strings.TrimSpace(info.FullMethod) != "" {
			boundary += ":" + info.FullMethod
		}
		callOptions := options
		callOptions.Boundary = boundary
		err := panicguard.Run(ctx, callOptions, func(context.Context) error {
			return handler(server, stream)
		})
		if err != nil {
			return ToStatus(err)
		}
		return nil
	}
}
