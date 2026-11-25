package tracecontext

import (
	"context"
	"testing"
)

func TestStartSpanExtractsIncomingTraceAndInjectsChildHeaders(t *testing.T) {
	parent := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx, span, err := StartSpan(context.Background(), SpanRequest{
		Kind: OperationGRPC, Component: "gateway", Operation: "mozi.gateway.Call",
		IncomingHeaders: map[string]string{TraceParentHeader: parent, TraceStateHeader: "vendor=value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if span.Trace.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || span.Trace.ParentSpanID != "00f067aa0ba902b7" || span.Trace.SpanID == "00f067aa0ba902b7" {
		t.Fatalf("child span 未正确继承 trace: %#v", span.Trace)
	}
	if span.Kind != OperationGRPC || span.Component != "gateway" || span.Operation != "mozi.gateway.Call" {
		t.Fatalf("span 元数据错误: %#v", span)
	}
	if span.OutgoingHeaders[TraceParentHeader] == "" || span.OutgoingHeaders[TraceParentHeader] == parent || span.OutgoingHeaders[TraceStateHeader] != "vendor=value" {
		t.Fatalf("trace headers 注入错误: %#v", span.OutgoingHeaders)
	}
	got, ok := FromContext(ctx)
	if !ok || got.TraceID != span.Trace.TraceID || got.SpanID != span.Trace.SpanID {
		t.Fatalf("context 未携带当前 span trace: trace=%#v ok=%v", got, ok)
	}
}

func TestStartSpanCreatesRootForWailsTaskSQLiteAndRemoteService(t *testing.T) {
	for _, kind := range []OperationKind{OperationWails, OperationTask, OperationSQLite, OperationRemoteService} {
		t.Run(string(kind), func(t *testing.T) {
			_, span, err := StartSpan(context.Background(), SpanRequest{
				Kind: kind, Component: "runtime", Operation: "operation.run", Sampled: true,
				SafeAttributes: map[string]string{"result": "ok"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if span.Trace.TraceID == "" || span.Trace.SpanID == "" || span.Trace.ParentSpanID != "" || !span.Trace.Sampled {
				t.Fatalf("root span 错误: %#v", span.Trace)
			}
			if span.OutgoingHeaders[TraceParentHeader] == "" {
				t.Fatalf("root span 未注入 traceparent: %#v", span.OutgoingHeaders)
			}
		})
	}
}

func TestStartSpanContinuesParentFromContext(t *testing.T) {
	parent, err := New(true)
	if err != nil {
		t.Fatal(err)
	}
	parentCtx, err := WithContext(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	_, span, err := StartSpan(parentCtx, SpanRequest{Kind: OperationTask, Component: "taskengine", Operation: "task.execute"})
	if err != nil {
		t.Fatal(err)
	}
	if span.Trace.TraceID != parent.TraceID || span.Trace.ParentSpanID != parent.SpanID || span.Trace.SpanID == parent.SpanID {
		t.Fatalf("应从 context 父 trace 生成 child: parent=%#v span=%#v", parent, span.Trace)
	}
}

func TestStartSpanNotifiesObserver(t *testing.T) {
	var got Span
	ctx := WithObserver(context.Background(), func(span Span) { got = span })
	_, span, err := StartSpan(ctx, SpanRequest{Kind: OperationSQLite, Component: "sqlite", Operation: "sqlite.open", Sampled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Trace.TraceID != span.Trace.TraceID || got.Operation != "sqlite.open" {
		t.Fatalf("observer 未收到 span: %#v", got)
	}
}

func TestStartSpanRejectsUnsafeMetadata(t *testing.T) {
	_, _, err := StartSpan(context.Background(), SpanRequest{
		Kind: OperationWails, Component: "desktop", Operation: "desktop.Open",
		SafeAttributes: map[string]string{"token": "must-not-pass"},
	})
	if err == nil {
		t.Fatal("trace middleware 必须拒绝敏感属性")
	}
}
