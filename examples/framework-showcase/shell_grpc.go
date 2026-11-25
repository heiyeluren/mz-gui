package main

import (
	"context"
	"fmt"

	"github.com/heiyeluren/mz-gui/framework/go/core/panicguard"
	"github.com/heiyeluren/mz-gui/framework/go/transport/grpcfault"
	"google.golang.org/grpc"
)

func probeShowcaseStreamBoundary(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("showcase gRPC 边界上下文无效")
	}
	interceptor := grpcfault.StreamServerBoundary(panicguard.Options{Boundary: "showcase.shell.grpc-stream"})
	err := interceptor(nil, &showcaseProbeStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: "/showcase.shell.v1.Service/Probe"}, func(any, grpc.ServerStream) error {
		return nil
	})
	if err != nil {
		return fmt.Errorf("showcase gRPC stream 边界失败: %w", err)
	}
	return nil
}

type showcaseProbeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *showcaseProbeStream) Context() context.Context {
	if stream == nil || stream.ctx == nil {
		return context.Background()
	}
	return stream.ctx
}
