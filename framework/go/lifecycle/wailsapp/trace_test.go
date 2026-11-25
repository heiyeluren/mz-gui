package wailsapp

import (
	"context"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func TestStartCommandSpanCreatesWailsRootSpan(t *testing.T) {
	ctx, span, err := StartCommandSpan(context.Background(), "desktop.OpenWorkspace")
	if err != nil {
		t.Fatal(err)
	}
	if span.Kind != tracecontext.OperationWails || span.Component != "wails.command" || span.Operation != "desktop.OpenWorkspace" {
		t.Fatalf("wails span 元数据错误: %#v", span)
	}
	got, ok := tracecontext.FromContext(ctx)
	if !ok || got.TraceID != span.Trace.TraceID || got.SpanID != span.Trace.SpanID {
		t.Fatalf("command context 未携带 span: %#v ok=%v", got, ok)
	}
}

func TestStartCommandSpanContinuesParentTrace(t *testing.T) {
	parent, err := tracecontext.New(true)
	if err != nil {
		t.Fatal(err)
	}
	parentCtx, err := tracecontext.WithContext(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	_, span, err := StartCommandSpan(parentCtx, "desktop.Save")
	if err != nil {
		t.Fatal(err)
	}
	if span.Trace.TraceID != parent.TraceID || span.Trace.ParentSpanID != parent.SpanID {
		t.Fatalf("Wails command 必须继承父 trace: parent=%#v span=%#v", parent, span.Trace)
	}
}
