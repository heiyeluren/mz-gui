package remoteservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/resilience"
)

func TestPolicyRejectsUnsafeRequest(t *testing.T) {
	policy := DefaultHTTPPolicy()
	policy.AllowedHosts = set("api.example.test")
	if _, err := policy.Validate(http.MethodGet, "file:///etc/passwd", nil, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("非法 URL 错误 = %v", err)
	}
	if _, err := policy.Validate(http.MethodGet, "https://evil.example.test/v1", nil, 0); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("host 白名单错误 = %v", err)
	}
	if _, err := policy.Validate("TRACE", "https://api.example.test/v1", nil, 0); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("method 白名单错误 = %v", err)
	}
	headers := http.Header{"X-Unsafe": []string{"value"}}
	if _, err := policy.Validate(http.MethodGet, "https://api.example.test/v1", headers, 0); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("header 白名单错误 = %v", err)
	}
	headers = http.Header{"Accept": []string{"text/plain\r\nInjected: yes"}}
	if _, err := policy.Validate(http.MethodGet, "https://api.example.test/v1", headers, 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("header 注入错误 = %v", err)
	}
}

func TestHTTPClientRetriesIdempotentRequestAndInjectsHeaders(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		count := calls.Add(1)
		if got := request.Header.Get("X-Request-ID"); got != "req-1" {
			t.Errorf("X-Request-ID = %q", got)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "idem-1" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if count == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte("retry"))
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		Policy: policyForServer(t, server.URL),
		Retry:  RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), Request{
		Method: http.MethodPost, URL: server.URL, Body: []byte("payload"),
		RequestID: "req-1", IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(response.Body) != "ok" || response.Attempts != 2 {
		t.Fatalf("响应不符合预期: %#v body=%q", response, response.Body)
	}
	if calls.Load() != 2 {
		t.Fatalf("调用次数 = %d，期望 2", calls.Load())
	}
}

func TestHTTPClientLimiterAndBreakerStopNetworkCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	defer server.Close()

	limiter, err := resilience.NewLimiter(resilience.LimiterConfig{
		Name: "remote-test", Capacity: 1, RefillTokens: 1, RefillInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Acquire(1); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL), Limiter: limiter})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := client.Do(ctx, Request{Method: http.MethodGet, URL: server.URL}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("限流错误 = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("限流后不应发出请求，实际 %d", calls.Load())
	}

	breaker, err := resilience.NewBreaker(resilience.BreakerConfig{
		Name: "remote-breaker", FailureThreshold: 1, SuccessThreshold: 1,
		OpenTimeout: time.Hour, HalfOpenMaxRequests: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := breaker.Allow()
	if err != nil {
		t.Fatal(err)
	}
	permit.Done(fmt.Errorf("offline"))
	client, err = NewClient(ClientOptions{Policy: policyForServer(t, server.URL), Breaker: breaker})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL}); !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("熔断错误 = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("熔断后不应发出请求，实际 %d", calls.Load())
	}
}

func TestParseSSESupportsFieldsAndLimits(t *testing.T) {
	events, err := ParseSSE(context.Background(), strings.NewReader("id: 7\nevent: patch\nretry: 250\ndata: hello\ndata: world\n\n"), SSEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "7" || events[0].Event != "patch" || events[0].Retry != 250*time.Millisecond || events[0].Data != "hello\nworld" {
		t.Fatalf("SSE 事件不符合预期: %#v", events)
	}
	if _, err := ParseSSE(context.Background(), strings.NewReader("data: 123456\n\n"), SSEOptions{MaxDataBytes: 3}); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("SSE data 限制错误 = %v", err)
	}
}

func TestDialWebSocketEcho(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		messageType, payload, err := conn.Read(request.Context())
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}
		if err := conn.Write(request.Context(), messageType, payload); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	rawURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := DialWebSocket(context.Background(), rawURL, nil, WebSocketOptions{Policy: websocketPolicyForServer(t, rawURL)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if err := conn.Write(context.Background(), websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, payload, err := conn.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "ping" {
		t.Fatalf("echo = %q", payload)
	}
}

func TestDialGRPCPolicy(t *testing.T) {
	if _, err := DialGRPC(context.Background(), GRPCOptions{Target: "remote.example.test:443", AllowInsecureLocal: true}); !errors.Is(err, ErrPolicyDenied) {
		t.Fatalf("非本机 insecure gRPC 错误 = %v", err)
	}
	conn, err := DialGRPC(context.Background(), GRPCOptions{Target: "127.0.0.1:1", AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func policyForServer(t *testing.T, rawURL string) Policy {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultHTTPPolicy()
	policy.AllowedHosts = set(parsed.Hostname())
	return policy
}

func websocketPolicyForServer(t *testing.T, rawURL string) Policy {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultWebSocketPolicy()
	policy.AllowedHosts = set(parsed.Hostname())
	return policy
}
