package local

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestPeerPolicyVerifiesAllowedIdentity(t *testing.T) {
	t.Parallel()
	policy := RequirePeerIdentity(PeerIdentity{Kind: "uid", Value: "501"})

	if err := policy.Verify(PeerIdentity{Kind: "uid", Value: "501"}); err != nil {
		t.Fatalf("合法 peer 身份被拒绝: %v", err)
	}
	if err := policy.Verify(PeerIdentity{Kind: "uid", Value: "502"}); err == nil {
		t.Fatal("不匹配 peer 身份未被拒绝")
	}
	if err := policy.Verify(PeerIdentity{Kind: "sid", Value: "501"}); err == nil {
		t.Fatal("不匹配 peer 身份类型未被拒绝")
	}
}

func TestPeerPolicyRejectsEmptyAllowedIdentity(t *testing.T) {
	t.Parallel()
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	_, err := NewServerWithOptions(endpoint, ServerOptions{PeerPolicy: PeerPolicy{Required: true}})
	if err == nil || !strings.Contains(err.Error(), "peer 身份") {
		t.Fatalf("空 peer 身份校验错误 = %v", err)
	}
}

func TestServerWithCurrentUserPeerPolicyAllowsHealthRoundTrip(t *testing.T) {
	endpoint := Endpoint{Name: "sample-agent", RunDir: filepath.Join(shortTempDir(t), "run")}
	identity, err := CurrentUserPeerIdentity()
	if err != nil {
		t.Fatalf("读取当前用户 peer 身份: %v", err)
	}
	server, err := NewServerWithOptions(endpoint, ServerOptions{PeerPolicy: RequirePeerIdentity(identity)})
	if err != nil {
		t.Fatalf("创建启用 peer 身份校验的服务: %v", err)
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
	if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("服务状态 = %s", response.GetStatus())
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
