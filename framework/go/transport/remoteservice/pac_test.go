package remoteservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePACDecisionParsesProxyAndDirect(t *testing.T) {
	directives, err := ParsePACDecision("PROXY proxy.example.test:8080; DIRECT")
	if err != nil {
		t.Fatal(err)
	}
	if len(directives) != 2 || directives[0].Action != PACActionProxy || directives[0].Host != "proxy.example.test" || directives[0].Port != 8080 {
		t.Fatalf("代理指令异常: %#v", directives)
	}
	if directives[1].Action != PACActionDirect {
		t.Fatalf("DIRECT 回退异常: %#v", directives[1])
	}
}

func TestPACResolverSelectsFirstUsableProxy(t *testing.T) {
	resolver, err := NewPACResolver(PACFinderFunc(func(*url.URL) (string, error) {
		return "SOCKS5 socks.example.test:1080; PROXY proxy.example.test:8080", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(&url.URL{Scheme: "https", Host: "service.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Direct || resolution.ProxyURL != "socks5://socks.example.test:1080" || resolution.Decision == "" {
		t.Fatalf("代理选择异常: %#v", resolution)
	}
}

func TestPACResolverStopsAtDirect(t *testing.T) {
	resolver, err := NewPACResolver(NewStaticPACFinder("DIRECT; PROXY proxy.example.test:8080"))
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := resolver.Resolve(&url.URL{Scheme: "https", Host: "intranet.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.Direct || resolution.ProxyURL != "" {
		t.Fatalf("DIRECT 必须立即直连: %#v", resolution)
	}
}

func TestParseConstantPACScriptRejectsBranchingScript(t *testing.T) {
	decision, err := ParseConstantPACScript(`function FindProxyForURL(url, host) { return "PROXY proxy.example.test:8080"; }`)
	if err != nil || decision != "PROXY proxy.example.test:8080" {
		t.Fatalf("常量 PAC 脚本解析异常: %q %v", decision, err)
	}
	if _, err := ParseConstantPACScript(`function FindProxyForURL(url, host) { if (host == "intranet") return "DIRECT"; return "PROXY proxy.example.test:8080"; }`); !errors.Is(err, ErrPACScriptUnsupported) {
		t.Fatalf("分支 PAC 脚本必须拒绝: %v", err)
	}
}

func TestWPADCandidatesRejectsUnsafeSuffix(t *testing.T) {
	candidates, err := WPADCandidates("corp.example.test")
	if err != nil || len(candidates) != 1 || candidates[0] != "http://wpad.corp.example.test/wpad.dat" {
		t.Fatalf("WPAD candidate 异常: %#v %v", candidates, err)
	}
	if _, err := WPADCandidates(""); !errors.Is(err, ErrInvalidPAC) {
		t.Fatalf("空 suffix 必须拒绝: %v", err)
	}
	if _, err := WPADCandidates("corp.example.test/../../secret"); !errors.Is(err, ErrInvalidPAC) {
		t.Fatalf("路径注入 suffix 必须拒绝: %v", err)
	}
}

func TestPACFetcherFetchesHTTPAndRejectsUnsafeURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/wpad.dat" {
			t.Fatalf("PAC 路径异常: %s", request.URL.Path)
		}
		io.WriteString(writer, `function FindProxyForURL(url, host) { return "DIRECT"; }`)
	}))
	defer server.Close()
	fetcher, err := NewPACFetcher(PACFetchOptions{Timeout: time.Second, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	document, err := fetcher.Fetch(context.Background(), server.URL+"/wpad.dat")
	if err != nil || !strings.Contains(document.Body, "FindProxyForURL") || document.SourceURL == "" || document.FetchedAt.IsZero() {
		t.Fatalf("PAC 获取异常: %#v %v", document, err)
	}
	if _, err := fetcher.Fetch(context.Background(), "file:///tmp/wpad.dat"); !errors.Is(err, ErrInvalidPAC) {
		t.Fatalf("file URL 必须拒绝: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), "http://user:secret@127.0.0.1/wpad.dat"); !errors.Is(err, ErrInvalidPAC) {
		t.Fatalf("带凭据 URL 必须拒绝: %v", err)
	}
}

func TestPACFetcherRejectsOversizedDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write(bytesRepeat('A', 64))
	}))
	defer server.Close()
	fetcher, err := NewPACFetcher(PACFetchOptions{Timeout: time.Second, MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/wpad.dat"); !errors.Is(err, ErrPACFetchFailed) {
		t.Fatalf("超大 PAC 必须拒绝: %v", err)
	}
}

func bytesRepeat(value byte, count int) []byte {
	body := make([]byte, count)
	for index := range body {
		body[index] = value
	}
	return body
}
