package tracecontext

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTraceContextParsesFormatsInjectsAndExtracts(t *testing.T) {
	trace, err := ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Sampled || trace.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || trace.SpanID != "00f067aa0ba902b7" {
		t.Fatalf("解析结果错误: %#v", trace)
	}
	parent, err := trace.TraceParent()
	if err != nil {
		t.Fatal(err)
	}
	if parent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("格式化错误: %s", parent)
	}
	headers := map[string]string{}
	trace.TraceState = "vendor=value"
	if err := trace.Inject(headers); err != nil {
		t.Fatal(err)
	}
	extracted, err := Extract(map[string]string{"TraceParent": headers[TraceParentHeader], "TraceState": headers[TraceStateHeader]})
	if err != nil {
		t.Fatal(err)
	}
	if extracted.TraceState != "vendor=value" || !extracted.Sampled {
		t.Fatalf("提取结果错误: %#v", extracted)
	}
}

func TestTraceContextRejectsInvalidTraceParent(t *testing.T) {
	for _, value := range []string{
		"",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz",
	} {
		if _, err := ParseTraceParent(value); !errors.Is(err, ErrInvalidTraceContext) {
			t.Fatalf("非法 traceparent 应失败: %q err=%v", value, err)
		}
	}
}

func TestTraceContextContextPropagationAndChildSpan(t *testing.T) {
	root, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.TraceID) != 32 || len(root.SpanID) != 16 || strings.Trim(root.TraceID, "0") == "" {
		t.Fatalf("新 trace id 错误: %#v", root)
	}
	ctx, err := WithContext(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := FromContext(ctx)
	if !ok || got.TraceID != root.TraceID {
		t.Fatalf("context 传播失败: %#v ok=%v", got, ok)
	}
	child, err := ChildOf(root)
	if err != nil {
		t.Fatal(err)
	}
	if child.TraceID != root.TraceID || child.ParentSpanID != root.SpanID || child.SpanID == root.SpanID {
		t.Fatalf("child span 错误: parent=%#v child=%#v", root, child)
	}
}
