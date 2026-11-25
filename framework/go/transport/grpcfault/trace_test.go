package grpcfault

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestUnaryTraceInjectsTraceParentOnHealthCheck(t *testing.T) {
	header := serveHealth(t, context.Background())
	got := firstMetadata(header, tracecontext.TraceParentHeader)
	if got == "" || !strings.HasPrefix(got, "00-") {
		t.Fatalf("gRPC 未注入 traceparent: %#v", header)
	}
}

func TestStreamTraceInjectsTraceParentOnHealthWatch(t *testing.T) {
	header := serveHealthWatch(t, context.Background())
	got := firstMetadata(header, tracecontext.TraceParentHeader)
	if got == "" || !strings.HasPrefix(got, "00-") {
		t.Fatalf("gRPC stream 未注入 traceparent: %#v", header)
	}
}

func TestUnaryTraceContinuesIncomingParentTrace(t *testing.T) {
	parent, err := tracecontext.New(true)
	if err != nil {
		t.Fatal(err)
	}
	incoming := map[string]string{}
	if err := parent.Inject(incoming); err != nil {
		t.Fatal(err)
	}
	md := metadata.MD{}
	for key, value := range incoming {
		md.Set(key, value)
	}
	header := serveHealth(t, metadata.NewOutgoingContext(context.Background(), md))
	got := firstMetadata(header, tracecontext.TraceParentHeader)
	if !strings.Contains(got, parent.TraceID) {
		t.Fatalf("gRPC 必须继承上游 TraceID: header=%q parent=%q", got, parent.TraceID)
	}
}

func serveHealth(t *testing.T, callCtx context.Context) metadata.MD {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryTrace()))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })

	var header metadata.MD
	rpcCtx, cancel := context.WithTimeout(callCtx, 3*time.Second)
	defer cancel()
	if _, err := grpc_health_v1.NewHealthClient(connection).Check(rpcCtx, &grpc_health_v1.HealthCheckRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	return header
}

func serveHealthWatch(t *testing.T, callCtx context.Context) metadata.MD {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainStreamInterceptor(StreamTrace()))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	connection, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })

	rpcCtx, cancel := context.WithTimeout(callCtx, 3*time.Second)
	defer cancel()
	watch, err := grpc_health_v1.NewHealthClient(connection).Watch(rpcCtx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watch.Recv(); err != nil {
		t.Fatal(err)
	}
	header, err := watch.Header()
	if err != nil {
		t.Fatal(err)
	}
	return header
}

func firstMetadata(header metadata.MD, key string) string {
	values := header.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
