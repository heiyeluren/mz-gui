package panicguard

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

type recordingReporter struct {
	incident Incident
	panicNow bool
}

func (reporter *recordingReporter) ReportPanic(_ context.Context, incident Incident) {
	reporter.incident = incident
	if reporter.panicNow {
		panic("reporter failed")
	}
}

type recordingMarker struct {
	boundary string
	reason   string
}

func (marker *recordingMarker) MarkDegraded(_ context.Context, boundary string, reason string) {
	marker.boundary = boundary
	marker.reason = reason
}

func TestRunConvertsPanicToInternalFaultAndMarksHealth(t *testing.T) {
	reporter := &recordingReporter{}
	marker := &recordingMarker{}
	err := Run(context.Background(), Options{Boundary: "worker.sample", RequestID: "request-1", TraceID: "trace-1", Reporter: reporter, Health: marker}, func(context.Context) error {
		panic("token=must-not-leak")
	})
	public := fault.Public(err)
	if public.Code != fault.CodeInternal || public.ReasonCode != "core.internal_panic" || public.RequestID != "request-1" {
		t.Fatalf("panic 错误契约异常: %#v", public)
	}
	if public.SafeDetails["boundary"] != "worker.sample" || public.SafeDetails["request_id"] != "request-1" || public.SafeDetails["trace_id"] != "trace-1" {
		t.Fatalf("panic SafeDetails 缺少 boundary/request_id/trace_id: %#v", public.SafeDetails)
	}
	if reporter.incident.Fingerprint == "" || reporter.incident.Boundary != "worker.sample" || marker.boundary != "worker.sample" || marker.reason != "core.internal_panic" {
		t.Fatalf("panic 诊断或健康降级缺失: incident=%#v marker=%#v", reporter.incident, marker)
	}
	if strings.Contains(err.Error(), "must-not-leak") || strings.Contains(string(reporter.incident.Stack), "must-not-leak") {
		t.Fatal("panic value 不得进入公开错误或诊断堆栈")
	}
}

func TestProduceMapsSentinelThroughMapperAndKeepsPanicContract(t *testing.T) {
	mapper := fault.NewMapper()
	if err := mapper.Bind(context.DeadlineExceeded, fault.Spec{
		Code: fault.CodeTimeout, ReasonCode: "core.deadline_exceeded",
		MessageKey: "errors.timeout", UserMessage: "操作超时", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Produce(context.Background(), mapper, Options{Boundary: "wails.settings"}, func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("成功路径不得返回 typed-nil: %v", err)
	}
	mapped := Produce(context.Background(), mapper, Options{Boundary: "wails.settings"}, func(context.Context) error {
		return context.DeadlineExceeded
	})
	public := fault.Public(mapped)
	if public.Code != fault.CodeTimeout || public.ReasonCode != "core.deadline_exceeded" {
		t.Fatalf("Produce 未走 Mapper: %#v", public)
	}

	panicked := Produce(nil, mapper, Options{Boundary: "wails.settings", RequestID: "request-9"}, func(context.Context) error {
		panic("must-not-leak")
	})
	panicPublic := fault.Public(panicked)
	if panicPublic.Code != fault.CodeInternal || panicPublic.ReasonCode != "core.internal_panic" || panicPublic.RequestID != "request-9" {
		t.Fatalf("Produce panic 契约异常: %#v", panicPublic)
	}
	if strings.Contains(fmt.Sprint(panicked), "must-not-leak") {
		t.Fatal("Produce 不得泄露 panic value")
	}
}

func TestProduceResultReturnsValueAndMapsErrors(t *testing.T) {
	mapper := fault.NewMapper()
	if err := mapper.Bind(context.DeadlineExceeded, fault.Spec{
		Code: fault.CodeTimeout, ReasonCode: "core.deadline_exceeded",
		MessageKey: "errors.timeout", UserMessage: "操作超时", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	value, err := ProduceResult(context.Background(), mapper, Options{Boundary: "wails.settings"}, func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil || value != "ok" {
		t.Fatalf("成功路径应返回值: value=%q err=%v", value, err)
	}
	value, err = ProduceResult(context.Background(), mapper, Options{Boundary: "wails.settings"}, func(context.Context) (string, error) {
		return "partial", context.DeadlineExceeded
	})
	public := fault.Public(err)
	if value != "" || public.Code != fault.CodeTimeout {
		t.Fatalf("失败路径应回零值并走 Mapper: value=%q public=%#v", value, public)
	}
	value, err = ProduceResult(context.Background(), mapper, Options{Boundary: "wails.settings"}, func(context.Context) (string, error) {
		panic("must-not-leak")
	})
	panicPublic := fault.Public(err)
	if value != "" || panicPublic.Code != fault.CodeInternal || strings.Contains(fmt.Sprint(err), "must-not-leak") {
		t.Fatalf("ProduceResult panic 契约异常: value=%q err=%v public=%#v", value, err, panicPublic)
	}
}

func TestProduceFillsIdentityFromContextUnlessOverridden(t *testing.T) {
	ctx := model.WithRequestID(context.Background(), "request-ctx")
	ctx = model.WithTraceID(ctx, "trace-ctx")
	err := Produce(ctx, nil, Options{Boundary: "wails.settings"}, func(context.Context) error {
		panic("boom")
	})
	public := fault.Public(err)
	if public.RequestID != "request-ctx" || public.TraceID != "trace-ctx" {
		t.Fatalf("应从 Context 回填身份: %#v", public)
	}
	err = Produce(ctx, nil, Options{Boundary: "wails.settings", RequestID: "request-opt", TraceID: "trace-opt"}, func(context.Context) error {
		panic("boom")
	})
	public = fault.Public(err)
	if public.RequestID != "request-opt" || public.TraceID != "trace-opt" {
		t.Fatalf("显式 Options 必须覆盖 Context: %#v", public)
	}

	now := time.Date(2026, 8, 16, 15, 30, 0, 0, time.UTC)
	deadline, err := model.NewTimestamp(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := model.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	ctx = model.WithRequestMeta(context.Background(), model.RequestMeta{
		RequestID: "request-meta", TraceID: "trace-meta", Deadline: deadline,
		Caller: model.Caller{Component: "desktop", Version: version, InstanceID: "desktop-1"},
	})
	err = Produce(ctx, nil, Options{Boundary: "wails.settings"}, func(context.Context) error { panic("boom") })
	public = fault.Public(err)
	if public.RequestID != "request-meta" || public.TraceID != "trace-meta" {
		t.Fatalf("RequestMeta 应回填身份: %#v", public)
	}
	if public.SafeDetails["caller"] != "desktop" || public.SafeDetails["boundary"] != "wails.settings" ||
		public.SafeDetails["request_id"] != "request-meta" || public.SafeDetails["trace_id"] != "trace-meta" {
		t.Fatalf("Caller/boundary/request_id/trace_id 应写入 SafeDetails: %#v", public.SafeDetails)
	}
}

func TestRunContainsReporterPanicAndPassesNormalErrors(t *testing.T) {
	reporter := &recordingReporter{panicNow: true}
	err := Run(context.Background(), Options{Boundary: "worker.sample", Reporter: reporter}, func(context.Context) error {
		panic("operation failed")
	})
	if fault.Normalize(err).Code() != fault.CodeInternal {
		t.Fatalf("reporter 二次 panic 后错误异常: %v", err)
	}
	normal := fault.Normalize(context.Canceled)
	if got := Run(context.Background(), Options{Boundary: "worker.sample"}, func(context.Context) error { return normal }); got != normal {
		t.Fatal("普通错误应原样返回")
	}
}
