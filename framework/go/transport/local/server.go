package local

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/transport/grpcfault"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	listener            net.Listener
	grpc                *grpc.Server
	gracefulStopTimeout time.Duration
}

type Registration func(grpc.ServiceRegistrar)

func NewServer(endpoint Endpoint, registrations ...Registration) (*Server, error) {
	return NewServerWithOptions(endpoint, ServerOptions{}, registrations...)
}

type ServerOptions struct {
	UnaryInterceptors   []grpc.UnaryServerInterceptor
	StreamInterceptors  []grpc.StreamServerInterceptor
	GracefulStopTimeout time.Duration
	PeerPolicy          PeerPolicy
}

func (options ServerOptions) gracefulStopTimeout() time.Duration {
	if options.GracefulStopTimeout == 0 {
		return 3 * time.Second
	}
	return options.GracefulStopTimeout
}

func NewServerWithOptions(endpoint Endpoint, options ServerOptions, registrations ...Registration) (*Server, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	if options.GracefulStopTimeout < 0 {
		return nil, fmt.Errorf("优雅停止超时不能为负数")
	}
	if err := options.PeerPolicy.Validate(); err != nil {
		return nil, err
	}
	listener, err := listen(endpoint)
	if err != nil {
		return nil, err
	}
	// Windows Named Pipe 在 Accept 时尚未收到客户端数据，ImpersonateNamedPipeClient 会失败并掐掉健康检查。
	// 管道 ACL 已限制为当前用户 SID，这里不再在 Accept 路径做二次模拟。Mac 仍走 peer verify。
	if options.PeerPolicy.Required && runtime.GOOS != "windows" {
		listener = &peerVerifyingListener{Listener: listener, policy: options.PeerPolicy}
	}
	interceptors := append([]grpc.UnaryServerInterceptor{grpcfault.UnaryTrace()}, options.UnaryInterceptors...)
	streamInterceptors := append([]grpc.StreamServerInterceptor{grpcfault.StreamTrace()}, options.StreamInterceptors...)
	serverOptions := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(interceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
	}
	grpcServer := grpc.NewServer(serverOptions...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	for _, register := range registrations {
		register(grpcServer)
	}
	return &Server{listener: listener, grpc: grpcServer, gracefulStopTimeout: options.gracefulStopTimeout()}, nil
}

// Serve 提供 gRPC 服务，并在取消时执行有界优雅关闭。
func (server *Server) Serve(ctx context.Context) error {
	errorsChannel := make(chan error, 1)
	go func() { errorsChannel <- server.grpc.Serve(server.listener) }()

	select {
	case err := <-errorsChannel:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() {
			server.grpc.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(server.gracefulStopTimeout):
			server.grpc.Stop()
		}
		_ = server.listener.Close()
		return nil
	}
}

func Dial(ctx context.Context, endpoint Endpoint) (*grpc.ClientConn, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	return grpc.NewClient(
		endpoint.Target(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return dial(ctx, endpoint)
		}),
	)
}
