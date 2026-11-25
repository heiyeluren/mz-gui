package main

import (
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	frameworkhealth "github.com/heiyeluren/mz-gui/framework/go/core/health"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

func TestNewShowcaseHealthUsesBuiltinRegistryAndServicePanicMarker(t *testing.T) {
	observedAt, err := model.NewTimestamp(time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	registry, marker, err := newShowcaseHealth(observedAt, frameworkclock.System{})
	if err != nil {
		t.Fatal(err)
	}
	if registry == nil || marker == nil {
		t.Fatal("showcase 必须拿到内置 registry 与 service panic marker")
	}
	build, err := model.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	report, err := registry.Snapshot(observedAt, build)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("内置健康检查数量异常: %#v", report)
	}
	found := false
	for _, check := range report.Checks {
		if check.CheckID.String() == frameworkhealth.ServiceReadinessCheckID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("内置 registry 缺少 %s", frameworkhealth.ServiceReadinessCheckID)
	}
	if _, _, err := newShowcaseHealth(observedAt, nil); err == nil {
		t.Fatal("空时钟必须拒绝")
	}
}
