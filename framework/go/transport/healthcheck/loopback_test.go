package healthcheck

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestLoopbackProbeAcceptsOnlySuccessfulLocalResponse(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/health" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNoContent)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	port := listener.Addr().(*net.TCPAddr).Port
	result, err := NewLoopbackProbe(Options{Timeout: time.Second}).Check(context.Background(), port, "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusNoContent || result.Endpoint == "" || result.CheckedAt.IsZero() || result.Latency < 0 {
		t.Fatalf("result=%#v", result)
	}
}

func TestLoopbackProbeBypassesProxyAndRejectsRedirect(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "https://example.com/", http.StatusFound)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	port := listener.Addr().(*net.TCPAddr).Port
	if _, err := NewLoopbackProbe(Options{Timeout: time.Second}).Check(context.Background(), port, "/api/health"); err == nil {
		t.Fatal("重定向必须被拒绝")
	}
	_ = os.Unsetenv("HTTP_PROXY")
}

func TestLoopbackProbeRejectsInvalidTargetAndHTTPFailure(t *testing.T) {
	probe := NewLoopbackProbe(Options{})
	for _, target := range []struct {
		port int
		path string
	}{{0, "/api/health"}, {65536, "/api/health"}, {18080, "api/health"}, {18080, "//example.com"}, {18080, "/api/health?token=x"}} {
		if _, err := probe.Check(context.Background(), target.port, target.path); err == nil {
			t.Fatalf("非法目标未拒绝: %#v", target)
		}
	}
	if _, err := probe.Check(nil, 18080, "/api/health"); err == nil {
		t.Fatal("nil context 必须拒绝")
	}
}
