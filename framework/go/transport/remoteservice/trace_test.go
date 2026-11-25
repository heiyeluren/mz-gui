package remoteservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func TestHTTPClientInjectsTraceParent(t *testing.T) {
	var gotTraceparent string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotTraceparent = request.Header.Get(tracecontext.TraceParentHeader)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL, RequestID: "req-trace"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("状态码异常: %d", response.StatusCode)
	}
	if gotTraceparent == "" || !strings.HasPrefix(gotTraceparent, "00-") {
		t.Fatalf("未注入 traceparent: %q", gotTraceparent)
	}
	if response.TraceID == "" || !strings.Contains(gotTraceparent, response.TraceID) {
		t.Fatalf("响应未带回 TraceID: resp=%q header=%q", response.TraceID, gotTraceparent)
	}
}

func TestHTTPClientContinuesIncomingTrace(t *testing.T) {
	parent, err := tracecontext.New(true)
	if err != nil {
		t.Fatal(err)
	}
	incoming := map[string]string{}
	if err := parent.Inject(incoming); err != nil {
		t.Fatal(err)
	}
	var gotTraceparent string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotTraceparent = request.Header.Get(tracecontext.TraceParentHeader)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{Policy: policyForServer(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	headers.Set(tracecontext.TraceParentHeader, incoming[tracecontext.TraceParentHeader])
	response, err := client.Do(context.Background(), Request{Method: http.MethodGet, URL: server.URL, Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	if response.TraceID != parent.TraceID {
		t.Fatalf("应继承上游 TraceID: got=%q want=%q", response.TraceID, parent.TraceID)
	}
	if !strings.Contains(gotTraceparent, parent.TraceID) {
		t.Fatalf("下游 header 未继承 TraceID: %q", gotTraceparent)
	}
}
