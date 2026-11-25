package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
	"google.golang.org/grpc"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	const root = "/tmp/mozi-test"
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("创建测试临时根目录: %v", err)
	}
	directory, err := os.MkdirTemp(root, "local-ipc-")
	if err != nil {
		t.Fatalf("创建测试临时目录: %v", err)
	}
	t.Cleanup(func() {
		// 该目录由本测试创建，且严格位于允许物理清理的测试临时根目录内。
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("清理测试临时目录: %v", err)
		}
	})
	return directory
}

func TestServerAndClientHealthRoundTrip(t *testing.T) {
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	server, err := NewServer(endpoint)
	if err != nil {
		t.Fatalf("创建本机 IPC 服务: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer dialCancel()
	connection, err := Dial(dialCtx, endpoint)
	if err != nil {
		cancel()
		t.Fatalf("连接本机 IPC: %v", err)
	}
	defer connection.Close()

	response, err := grpc_health_v1.NewHealthClient(connection).Check(dialCtx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		cancel()
		t.Fatalf("调用健康检查: %v", err)
	}
	if response.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("服务状态 = %s", response.Status)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("停止 IPC 服务: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("IPC 服务未按时停止")
	}
}

func TestServerRunsConfiguredUnaryInterceptor(t *testing.T) {
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	called := make(chan string, 1)
	server, err := NewServerWithOptions(endpoint, ServerOptions{UnaryInterceptors: []grpc.UnaryServerInterceptor{
		func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			called <- info.FullMethod
			return handler(ctx, request)
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Second)
	connection, err := Dial(dialCtx, endpoint)
	if err != nil {
		dialCancel()
		cancel()
		t.Fatal(err)
	}
	_, err = grpc_health_v1.NewHealthClient(connection).Check(dialCtx, &grpc_health_v1.HealthCheckRequest{})
	connection.Close()
	dialCancel()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	select {
	case method := <-called:
		if method != "/grpc.health.v1.Health/Check" {
			t.Fatalf("拦截方法 = %q", method)
		}
	case <-time.After(time.Second):
		t.Fatal("一元拦截器未执行")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerRunsConfiguredStreamInterceptor(t *testing.T) {
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	called := make(chan string, 1)
	server, err := NewServerWithOptions(endpoint, ServerOptions{StreamInterceptors: []grpc.StreamServerInterceptor{
		func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			called <- info.FullMethod
			return handler(server, stream)
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	dialCtx, dialCancel := context.WithTimeout(context.Background(), time.Second)
	connection, err := Dial(dialCtx, endpoint)
	if err != nil {
		dialCancel()
		cancel()
		t.Fatal(err)
	}
	watch, err := grpc_health_v1.NewHealthClient(connection).Watch(dialCtx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		connection.Close()
		dialCancel()
		cancel()
		t.Fatal(err)
	}
	if _, err := watch.Recv(); err != nil {
		connection.Close()
		dialCancel()
		cancel()
		t.Fatal(err)
	}
	connection.Close()
	dialCancel()
	select {
	case method := <-called:
		if method != "/grpc.health.v1.Health/Watch" {
			t.Fatalf("拦截方法 = %q", method)
		}
	case <-time.After(time.Second):
		t.Fatal("流拦截器未执行")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServerDefaultUnaryTraceInjectsTraceParent(t *testing.T) {
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	server, err := NewServer(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	connection, err := Dial(dialCtx, endpoint)
	if err != nil {
		dialCancel()
		cancel()
		t.Fatal(err)
	}
	var header metadata.MD
	_, err = grpc_health_v1.NewHealthClient(connection).Check(dialCtx, &grpc_health_v1.HealthCheckRequest{}, grpc.Header(&header))
	connection.Close()
	dialCancel()
	cancel()
	if waitErr := <-done; waitErr != nil {
		t.Fatal(waitErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	if values := header.Get(tracecontext.TraceParentHeader); len(values) > 0 {
		got = values[0]
	}
	if got == "" || !strings.HasPrefix(got, "00-") {
		t.Fatalf("默认本机 gRPC 必须注入 traceparent: %#v", header)
	}
}

func TestServerRejectsNegativeGracefulStopTimeout(t *testing.T) {
	t.Parallel()
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	_, err := NewServerWithOptions(endpoint, ServerOptions{GracefulStopTimeout: -time.Second})
	if err == nil {
		t.Fatal("负数优雅停止超时未被拒绝")
	}
}
