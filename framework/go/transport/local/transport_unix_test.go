//go:build darwin || linux

package local

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestDarwinSocketPathFallsBackForLongRunDir(t *testing.T) {
	t.Parallel()
	runDir := filepath.Join(shortTempDir(t), strings.Repeat("very-long-runtime-segment-", 5), "run")
	endpoint := Endpoint{Name: "sample-agent", RunDir: runDir}
	direct := filepath.Join(runDir, endpoint.Name+".sock")
	if len([]byte(direct)) <= maxUnixSocketPathBytes {
		t.Fatalf("测试路径必须超过 UDS 限制: %d", len([]byte(direct)))
	}
	socketPath, err := darwinSocketPath(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if socketPath == direct || len([]byte(socketPath)) > maxUnixSocketPathBytes {
		t.Fatalf("长路径必须映射为短 UDS: direct=%q socket=%q", direct, socketPath)
	}

	server, err := NewServer(endpoint)
	if err != nil {
		t.Fatalf("长运行根必须能创建本机 IPC 服务: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	connection, err := Dial(dialCtx, endpoint)
	if err != nil {
		dialCancel()
		cancel()
		t.Fatalf("长运行根必须能连接本机 IPC 服务: %v", err)
	}
	response, err := grpc_health_v1.NewHealthClient(connection).Check(dialCtx, &grpc_health_v1.HealthCheckRequest{})
	_ = connection.Close()
	dialCancel()
	cancel()
	if waitErr := <-done; waitErr != nil {
		t.Fatal(waitErr)
	}
	if err != nil || response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("长运行根健康检查失败: response=%v err=%v", response, err)
	}
}
