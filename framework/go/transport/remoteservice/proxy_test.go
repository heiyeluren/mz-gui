package remoteservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProxyDiagnosticProbeClassifiesProxyAuthentication(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.String() != "http://service.example.test/health" {
			t.Fatalf("代理收到的目标 URL 异常: %s", request.URL.String())
		}
		writer.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
		writer.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()
	probe, err := NewProxyDiagnosticProbe(ProxyDiagnosticOptions{
		TargetURL: "http://service.example.test/health",
		ProxyURL:  proxy.URL,
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ProxyStatusAuthRequired || !result.RequiresAuthentication || result.AuthScheme != "Basic" || result.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("代理认证诊断异常: %#v", result)
	}
	if result.TargetHost != "service.example.test" || result.ProxyURL == "" || result.CheckedAt.IsZero() {
		t.Fatalf("代理诊断元数据异常: %#v", result)
	}
}

func TestProxyDiagnosticProbeReportsAvailableAndUnavailable(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	probe, err := NewProxyDiagnosticProbe(ProxyDiagnosticOptions{
		TargetURL: "http://service.example.test/health",
		ProxyURL:  proxy.URL,
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ProxyStatusAvailable || result.StatusCode != http.StatusNoContent {
		t.Fatalf("代理可用诊断异常: %#v", result)
	}

	badProxyURL := (&url.URL{Scheme: "http", Host: "127.0.0.1:1"}).String()
	probe, err = NewProxyDiagnosticProbe(ProxyDiagnosticOptions{
		TargetURL: "http://service.example.test/health",
		ProxyURL:  badProxyURL,
		Timeout:   time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = probe.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != ProxyStatusUnavailable || result.Reason != "remote.proxy_unavailable" {
		t.Fatalf("代理不可用诊断异常: %#v", result)
	}
}

func TestProxyDiagnosticProbeRejectsInvalidConfig(t *testing.T) {
	if _, err := NewProxyDiagnosticProbe(ProxyDiagnosticOptions{TargetURL: "file:///tmp/a", ProxyURL: "http://127.0.0.1:1"}); !errors.Is(err, ErrInvalidProxyDiagnostic) {
		t.Fatalf("非法目标 URL 必须拒绝: %v", err)
	}
	if _, err := NewProxyDiagnosticProbe(ProxyDiagnosticOptions{TargetURL: "http://service.example.test", ProxyURL: "file:///tmp/a"}); !errors.Is(err, ErrInvalidProxyDiagnostic) {
		t.Fatalf("非法代理 URL 必须拒绝: %v", err)
	}
}
