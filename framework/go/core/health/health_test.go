package health

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestRegistryStartsUnknownAndReturnsStableSnapshot(t *testing.T) {
	initial := timestamp(t, "2026-08-15T01:00:00Z")
	registry, err := NewRegistry([]Definition{
		definition("core.service.readiness", KindReadiness, true),
		definition("core.runtime.liveness", KindLiveness, true),
		definition("core.storage.dependency", KindDependency, true),
	}, initial)
	if err != nil {
		t.Fatal(err)
	}
	build, _ := model.ParseVersion("1.0.0-beta.1+stage51")
	report, err := registry.Snapshot(timestamp(t, "2026-08-15T01:00:01Z"), build)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusUnknown || report.Build.String() != "1.0.0-beta.1+stage51" || len(report.Checks) != 3 {
		t.Fatalf("初始健康报告异常: %#v", report)
	}
	wantIDs := []model.ID{"core.runtime.liveness", "core.service.readiness", "core.storage.dependency"}
	gotIDs := make([]model.ID, 0, len(report.Checks))
	for _, check := range report.Checks {
		gotIDs = append(gotIDs, check.CheckID)
		if check.Status != StatusUnknown || check.ReasonCode != ReasonNotObserved || !check.ObservedAt.Equal(initial) {
			t.Fatalf("初始检查状态异常: %#v", check)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("检查项顺序不稳定: got=%v want=%v", gotIDs, wantIDs)
	}

	report.Checks[0].ReasonCode = "changed.by.caller"
	again, err := registry.Snapshot(timestamp(t, "2026-08-15T01:00:02Z"), build)
	if err != nil || again.Checks[0].ReasonCode != ReasonNotObserved {
		t.Fatalf("Snapshot 不得泄露内部可变切片: report=%#v err=%v", again, err)
	}
}

func TestRegistryAggregatesHealthKindsWithoutHidingReadiness(t *testing.T) {
	initial := timestamp(t, "2026-08-15T01:00:00Z")
	registry, err := NewRegistry([]Definition{
		definition("core.runtime.liveness", KindLiveness, true),
		definition("core.service.readiness", KindReadiness, true),
		definition("core.storage.dependency", KindDependency, true),
		definition("core.optional.dependency", KindDependency, false),
	}, initial)
	if err != nil {
		t.Fatal(err)
	}
	build, _ := model.ParseVersion("1.0.0")
	observed := timestamp(t, "2026-08-15T01:00:01Z")

	update(t, registry, "core.runtime.liveness", KindLiveness, StatusHealthy, ReasonOK, observed)
	update(t, registry, "core.service.readiness", KindReadiness, StatusHealthy, ReasonOK, observed)
	update(t, registry, "core.storage.dependency", KindDependency, StatusHealthy, ReasonOK, observed)
	report, err := registry.Snapshot(observed, build)
	if err != nil || report.Status != StatusHealthy {
		t.Fatalf("可选检查未观测不应阻止整体健康: %#v err=%v", report, err)
	}

	degradedAt := timestamp(t, "2026-08-15T01:00:02Z")
	update(t, registry, "core.storage.dependency", KindDependency, StatusUnhealthy, "storage.unavailable", degradedAt)
	report, _ = registry.Snapshot(degradedAt, build)
	if report.Status != StatusDegraded {
		t.Fatalf("依赖故障应降级但不伪装为进程死亡: %#v", report)
	}
	dependencyReport, err := registry.SnapshotKind(KindDependency, degradedAt, build)
	if err != nil || dependencyReport.Status != StatusDegraded || len(dependencyReport.Checks) != 2 {
		t.Fatalf("dependency 独立报告异常: %#v err=%v", dependencyReport, err)
	}
	livenessReport, err := registry.SnapshotKind(KindLiveness, degradedAt, build)
	if err != nil || livenessReport.Status != StatusHealthy || len(livenessReport.Checks) != 1 {
		t.Fatalf("liveness 不应被依赖故障伪装: %#v err=%v", livenessReport, err)
	}

	unhealthyAt := timestamp(t, "2026-08-15T01:00:03Z")
	update(t, registry, "core.service.readiness", KindReadiness, StatusUnhealthy, "core.not_ready", unhealthyAt)
	report, _ = registry.Snapshot(unhealthyAt, build)
	if report.Status != StatusUnhealthy {
		t.Fatalf("必需 readiness 故障必须使整体 unhealthy: %#v", report)
	}

	unknownAt := timestamp(t, "2026-08-15T01:00:04Z")
	update(t, registry, "core.service.readiness", KindReadiness, StatusUnknown, "core.probe_pending", unknownAt)
	report, _ = registry.Snapshot(unknownAt, build)
	if report.Status != StatusUnknown {
		t.Fatalf("必需检查未知应优先于普通降级: %#v", report)
	}
}

func TestRegistryRejectsInvalidAndOutOfOrderObservations(t *testing.T) {
	initial := timestamp(t, "2026-08-15T01:00:00Z")
	registry, err := NewRegistry([]Definition{definition("core.runtime.liveness", KindLiveness, true)}, initial)
	if err != nil {
		t.Fatal(err)
	}
	observed := timestamp(t, "2026-08-15T01:00:02Z")
	check := Check{CheckID: "core.runtime.liveness", Kind: KindLiveness, Status: StatusHealthy, ReasonCode: ReasonOK, ObservedAt: observed}
	if err := registry.Update(check); err != nil {
		t.Fatal(err)
	}
	if err := registry.Update(check); err != nil {
		t.Fatalf("相同观测重放应幂等: %v", err)
	}
	conflict := check
	conflict.Status = StatusDegraded
	conflict.ReasonCode = "core.slow"
	if err := registry.Update(conflict); !errors.Is(err, ErrObservationConflict) {
		t.Fatalf("同时间冲突必须拒绝: %v", err)
	}
	stale := check
	stale.ObservedAt = timestamp(t, "2026-08-15T01:00:01Z")
	if err := registry.Update(stale); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("旧观测必须拒绝: %v", err)
	}
	unknown := check
	unknown.CheckID = "core.unknown.liveness"
	if err := registry.Update(unknown); !errors.Is(err, ErrCheckNotRegistered) {
		t.Fatalf("未知检查必须拒绝: %v", err)
	}
	wrongKind := check
	wrongKind.Kind = KindReadiness
	if err := registry.Update(wrongKind); !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("检查类型漂移必须拒绝: %v", err)
	}
	invalidReason := check
	invalidReason.ObservedAt = timestamp(t, "2026-08-15T01:00:03Z")
	invalidReason.ReasonCode = "token=secret"
	if err := registry.Update(invalidReason); !errors.Is(err, ErrInvalidCheck) {
		t.Fatalf("自由文本 reason 必须拒绝: %v", err)
	}
	inconsistent := check
	inconsistent.ObservedAt = timestamp(t, "2026-08-15T01:00:03Z")
	inconsistent.Status = StatusHealthy
	inconsistent.ReasonCode = "core.slow"
	if err := registry.Update(inconsistent); !errors.Is(err, ErrInvalidCheck) {
		t.Fatalf("healthy 状态必须使用稳定成功原因: %v", err)
	}
	unregisteredReason := check
	unregisteredReason.ObservedAt = timestamp(t, "2026-08-15T01:00:03Z")
	unregisteredReason.Status = StatusDegraded
	unregisteredReason.ReasonCode = "core.not_registered"
	if err := registry.Update(unregisteredReason); !errors.Is(err, ErrReasonNotRegistered) {
		t.Fatalf("未登记 reason 必须拒绝: %v", err)
	}
	build, _ := model.ParseVersion("1.0.0")
	if _, err := registry.Snapshot(initial, build); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("报告时间不得早于已有观测: %v", err)
	}
}

func TestRegistrySupportsConcurrentReadersAndWriters(t *testing.T) {
	initial := timestamp(t, "2026-08-15T01:00:00Z")
	registry, err := NewRegistry([]Definition{
		definition("core.runtime.liveness", KindLiveness, true),
		definition("core.service.readiness", KindReadiness, true),
	}, initial)
	if err != nil {
		t.Fatal(err)
	}
	build, _ := model.ParseVersion("1.0.0")
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			for index := 1; index <= 100; index++ {
				observedAt, _ := model.NewTimestamp(initial.Time().Add(time.Duration(worker*100+index) * time.Nanosecond))
				_ = registry.Update(Check{CheckID: "core.runtime.liveness", Kind: KindLiveness, Status: StatusHealthy, ReasonCode: ReasonOK, ObservedAt: observedAt})
				_, _ = registry.Snapshot(timestamp(t, "2026-08-15T01:01:00Z"), build)
			}
		}(worker)
	}
	wait.Wait()
	report, err := registry.Snapshot(timestamp(t, "2026-08-15T01:01:00Z"), build)
	if err != nil || len(report.Checks) != 2 {
		t.Fatalf("并发后报告异常: %#v err=%v", report, err)
	}
}

func TestCatalogIsStrictVersionedAndEmbedded(t *testing.T) {
	catalog, err := BuiltinCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaVersion != SchemaVersion || catalog.Release != "1.0.0" || len(catalog.Definitions()) != 3 {
		t.Fatalf("内置健康 Catalog 身份异常: %#v", catalog)
	}
	for _, id := range []string{"core.runtime.liveness", "core.service.readiness", "core.storage.dependency"} {
		if _, ok := catalog.Lookup(id); !ok {
			t.Errorf("内置健康 Catalog 缺少 %s", id)
		}
	}
	data, err := os.ReadFile("v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("仓库健康 Catalog 无效: %v", err)
	}
	invalidCases := [][]byte{
		[]byte(`{"schema_version":1,"release":"1.0.0","unexpected":true,"checks":[]}`),
		[]byte(`{"schema_version":1,"release":"1.0.0","checks":[{"check_id":"core.runtime.liveness","kind":"liveness","required":true,"description_key":"health.runtime_liveness","owner":"framework-core"},{"check_id":"core.runtime.liveness","kind":"liveness","required":true,"description_key":"health.runtime_liveness","owner":"framework-core"}]}`),
		[]byte(`{"schema_version":1,"release":"1.0.0","checks":[{"check_id":"core.runtime.liveness","kind":"alive","required":true,"description_key":"health.runtime_liveness","owner":"framework-core"}]}`),
	}
	for _, candidate := range invalidCases {
		if _, err := Decode(candidate); !errors.Is(err, ErrInvalidCatalog) {
			t.Errorf("非法健康 Catalog 应被拒绝: %v", err)
		}
	}
}

func TestBuiltinRegistryAndServicePanicMarker(t *testing.T) {
	initial := timestamp(t, "2026-08-16T15:00:00Z")
	registry, err := NewBuiltinRegistry(initial)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 15, 0, 1, 0, time.UTC)
	marker, err := NewServicePanicMarker(registry, fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	marker.MarkDegraded(context.Background(), "SettingsService.Load", "core.internal_panic")
	build, _ := model.ParseVersion("1.0.0")
	report, err := registry.Snapshot(timestamp(t, "2026-08-16T15:00:02Z"), build)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, check := range report.Checks {
		if check.CheckID.String() != ServiceReadinessCheckID {
			continue
		}
		found = true
		if check.Status != StatusDegraded || check.ReasonCode != "core.internal_panic" {
			t.Fatalf("内置 readiness panic 未降级: %#v", check)
		}
	}
	if !found {
		t.Fatalf("内置 registry 缺少 %s: %#v", ServiceReadinessCheckID, report.Checks)
	}
}

func TestPanicMarkerUpdatesRegisteredCheck(t *testing.T) {
	initial := timestamp(t, "2026-08-15T01:00:00Z")
	registry, err := NewRegistry([]Definition{definition("core.runtime.liveness", KindLiveness, true)}, initial)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 1, 0, 1, 0, time.UTC)
	marker, err := NewPanicMarker(registry, "core.runtime.liveness", fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	marker.MarkDegraded(context.Background(), "worker.sample", "core.internal_panic")
	build, _ := model.ParseVersion("1.0.0")
	report, err := registry.Snapshot(timestamp(t, "2026-08-15T01:00:02Z"), build)
	if err != nil || report.Checks[0].Status != StatusDegraded || report.Checks[0].ReasonCode != "core.internal_panic" {
		t.Fatalf("panic marker 未写入降级状态: %#v err=%v", report, err)
	}
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time                       { return clock.now }
func (clock fixedClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }
func (clock fixedClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return nil, errors.New("测试不支持 ticker")
}

func definition(id string, kind Kind, required bool) Definition {
	return Definition{
		CheckID: id, Kind: kind, Required: required, DescriptionKey: "health." + id, Owner: "framework-core",
		ReasonCodes: []string{ReasonNotObserved, ReasonOK, "core.internal_panic", "core.not_ready", "core.probe_pending", "core.slow", "storage.unavailable"},
	}
}

func timestamp(t *testing.T, value string) model.Timestamp {
	t.Helper()
	result, err := model.ParseTimestamp(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func update(t *testing.T, registry *Registry, id string, kind Kind, status Status, reason string, observedAt model.Timestamp) {
	t.Helper()
	if err := registry.Update(Check{CheckID: model.ID(id), Kind: kind, Status: status, ReasonCode: reason, ObservedAt: observedAt}); err != nil {
		t.Fatal(err)
	}
}
