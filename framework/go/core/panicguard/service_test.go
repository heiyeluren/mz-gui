package panicguard

import (
	"context"
	"errors"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/fault"
	"github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestServiceGuardMapsDefaultsAndReturnsValue(t *testing.T) {
	service, err := NewService("SettingsService")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Guard(context.Background(), "Save", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("成功路径失败: %v", err)
	}
	err = service.Guard(context.Background(), "Save", func(context.Context) error { return context.Canceled })
	public := fault.Public(err)
	if public.Code != fault.CodeCancelled || public.ReasonCode != "core.operation_cancelled" {
		t.Fatalf("默认取消映射异常: %#v", public)
	}
	value, err := GuardResult(context.Background(), service, "Load", func(context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil || value != "ok" {
		t.Fatalf("GuardResult 成功路径异常: value=%q err=%v", value, err)
	}
}

func TestServiceGuardPanicMarksHealth(t *testing.T) {
	service, err := NewService("SettingsService")
	if err != nil {
		t.Fatal(err)
	}
	marker := &recordingMarker{}
	reporter := &recordingReporter{}
	guarded := service.WithHealth(marker).WithReporter(reporter)
	err = guarded.Guard(context.Background(), "Load", func(context.Context) error {
		panic("must-not-leak")
	})
	public := fault.Public(err)
	if public.Code != fault.CodeInternal || marker.boundary != "SettingsService.Load" || marker.reason != "core.internal_panic" {
		t.Fatalf("Guard panic 未降级健康: public=%#v marker=%#v", public, marker)
	}
	if public.SafeDetails["boundary"] != "SettingsService.Load" || public.SafeDetails["caller"] != "SettingsService" {
		t.Fatalf("Guard panic SafeDetails 缺少 boundary/caller: %#v", public.SafeDetails)
	}
	ctx := model.WithCaller(context.Background(), model.Caller{Component: "desktop"})
	err = guarded.Guard(ctx, "Load", func(context.Context) error { panic("again") })
	public = fault.Public(err)
	if public.SafeDetails["caller"] != "desktop" || public.SafeDetails["boundary"] != "SettingsService.Load" {
		t.Fatalf("Context Caller 必须覆盖服务名: %#v", public.SafeDetails)
	}
	if reporter.incident.Boundary != "SettingsService.Load" || reporter.incident.Fingerprint == "" {
		t.Fatalf("Guard panic 未报告: %#v", reporter.incident)
	}
}

func TestProduceTakesHealthFromContextUnlessOverridden(t *testing.T) {
	fromCtx := &recordingMarker{}
	explicit := &recordingMarker{}
	ctx := WithHealthMarker(context.Background(), fromCtx)
	_ = Produce(ctx, nil, Options{Boundary: "wails.settings"}, func(context.Context) error {
		panic("boom")
	})
	if fromCtx.boundary != "wails.settings" || fromCtx.reason != "core.internal_panic" {
		t.Fatalf("Context 健康标记未回填: %#v", fromCtx)
	}
	_ = Produce(ctx, nil, Options{Boundary: "wails.settings", Health: explicit}, func(context.Context) error {
		panic("boom")
	})
	if explicit.boundary != "wails.settings" || fromCtx.boundary != "wails.settings" {
		t.Fatalf("显式 Health 必须覆盖 Context: explicit=%#v ctx=%#v", explicit, fromCtx)
	}
}

func TestAttachPanicHealthWritesRegistry(t *testing.T) {
	now := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	observed, err := model.NewTimestamp(now)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := health.NewRegistry([]health.Definition{{
		CheckID: "core.service.readiness", Kind: health.KindReadiness, Required: true,
		DescriptionKey: "health.core_service_readiness", Owner: "framework-core",
		ReasonCodes: []string{health.ReasonNotObserved, health.ReasonOK, "core.internal_panic"},
	}}, observed)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService("SettingsService")
	if err != nil {
		t.Fatal(err)
	}
	clockNow := now.Add(time.Second)
	guarded, err := AttachPanicHealth(service, registry, "core.service.readiness", attachClock{now: clockNow})
	if err != nil {
		t.Fatal(err)
	}
	_ = guarded.Guard(context.Background(), "Load", func(context.Context) error { panic("boom") })
	build, _ := model.ParseVersion("1.0.0")
	later, err := model.NewTimestamp(clockNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.Snapshot(later, build)
	if err != nil || len(report.Checks) != 1 || report.Checks[0].Status != health.StatusDegraded || report.Checks[0].ReasonCode != "core.internal_panic" {
		t.Fatalf("AttachPanicHealth 未写入 registry: %#v err=%v", report, err)
	}
}

type attachClock struct{ now time.Time }

func (clock attachClock) Now() time.Time                       { return clock.now }
func (clock attachClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }
func (clock attachClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return nil, errors.New("测试不支持 ticker")
}

func TestServiceGuardRequestBindsMetaAndMapsInvalid(t *testing.T) {
	service, err := NewService("SettingsService")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	meta := testRequestMeta(t, now)
	var seenRequest string
	if err := service.GuardRequest(context.Background(), now, meta, "Save", func(ctx context.Context) error {
		id, ok := model.RequestIDFromContext(ctx)
		if !ok {
			t.Fatal("GuardRequest 必须绑定 RequestID")
		}
		seenRequest = id.String()
		return nil
	}); err != nil {
		t.Fatalf("合法 RequestMeta 应通过: %v", err)
	}
	if seenRequest != "request-guard" {
		t.Fatalf("RequestID 绑定异常: %q", seenRequest)
	}
	value, err := GuardRequestResult(context.Background(), service, now, meta, "Load", func(ctx context.Context) (string, error) {
		return "ok", nil
	})
	if err != nil || value != "ok" {
		t.Fatalf("GuardRequestResult 成功路径异常: value=%q err=%v", value, err)
	}
	err = service.GuardRequest(context.Background(), now, model.RequestMeta{}, "Save", func(context.Context) error {
		t.Fatal("非法 RequestMeta 不得进入业务")
		return nil
	})
	public := fault.Public(err)
	if public.Code != fault.CodeInvalidArgument || public.ReasonCode != "core.invalid_argument" {
		t.Fatalf("非法 RequestMeta 必须映射为公开错误: %#v", public)
	}
}

func TestNewServiceRejectsEmptyName(t *testing.T) {
	if _, err := NewService("  "); err == nil {
		t.Fatal("空服务名必须拒绝")
	}
}

func testRequestMeta(t *testing.T, now time.Time) model.RequestMeta {
	t.Helper()
	deadline, err := model.NewTimestamp(now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	version, err := model.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return model.RequestMeta{
		RequestID: "request-guard", TraceID: "trace-guard", Deadline: deadline,
		Caller: model.Caller{Component: "desktop", Version: version, InstanceID: "desktop-1"},
	}
}
