package nativee2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	frameworklocal "github.com/heiyeluren/mz-gui/framework/go/transport/local"
)

func TestGRPCHealthProbeUsesRealLocalTransport(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// UDS 在 macOS 上有严格长度限制，测试根固定落在允许清理的短临时目录。
	temporaryRoot := filepath.Join(string(filepath.Separator), "tmp", "mozi-test")
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runDir, err := os.MkdirTemp(temporaryRoot, "t336-health-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })
	endpoint := frameworklocal.Endpoint{Name: "native-e2e-health", RunDir: runDir}
	server, err := frameworklocal.NewServer(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	probe := GRPCHealthProbe{ProbeID: "agentd.grpc-health", Endpoint: endpoint}
	var detail string
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		detail, err = probe.Check(ctx)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || detail != "SERVING" {
		t.Fatalf("真实本机 gRPC health 应通过: detail=%q err=%v", detail, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("本机 gRPC server 未停止")
	}
}
