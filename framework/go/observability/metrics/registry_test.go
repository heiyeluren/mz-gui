package metrics

import (
	"errors"
	"testing"
)

func TestRegistryRegistersNormalizesAndListsMetrics(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Descriptor{
		Name:        "MZGUI_RUNTIME_QUEUE_LENGTH",
		Kind:        Gauge,
		Unit:        "COUNT",
		Description: "队列长度",
		Labels:      []string{"component", "process_role", "component"},
	})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := registry.Get("mzgui_runtime_queue_length")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Name != "mzgui_runtime_queue_length" || descriptor.Unit != "count" || len(descriptor.Labels) != 2 {
		t.Fatalf("指标规范化错误: %#v", descriptor)
	}
	if len(registry.List()) != 1 {
		t.Fatalf("指标列表错误: %#v", registry.List())
	}
}

func TestRegistryRejectsDuplicateAndInvalidMetrics(t *testing.T) {
	registry := NewRegistry()
	descriptor := Descriptor{
		Name:        "mzgui_remote_request_total",
		Kind:        Counter,
		Unit:        "count",
		Description: "远程请求数量",
		Labels:      []string{"component", "result"},
	}
	if err := registry.Register(descriptor); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(descriptor); !errors.Is(err, ErrMetricExists) {
		t.Fatalf("重复指标应失败，实际为 %v", err)
	}
	tests := []Descriptor{
		{Name: "remote_request_total", Kind: Counter, Unit: "count", Description: "missing prefix"},
		{Name: "mzgui_remote_request_total", Kind: "timer", Unit: "ms", Description: "bad kind"},
		{Name: "mzgui_remote_request_total", Kind: Counter, Unit: "", Description: "missing unit"},
		{Name: "mzgui_remote_request_total", Kind: Counter, Unit: "count", Description: "bad label", Labels: []string{"request_id"}},
		{Name: "mzgui_remote_request_total", Kind: Counter, Unit: "count", Description: "unknown label", Labels: []string{"tenant"}},
	}
	for _, candidate := range tests {
		if _, err := Normalize(candidate); !errors.Is(err, ErrInvalidDescriptor) {
			t.Fatalf("非法指标应失败: %#v err=%v", candidate, err)
		}
	}
}

func TestRegistryGetMissingMetric(t *testing.T) {
	if _, err := NewRegistry().Get("mzgui_missing_total"); !errors.Is(err, ErrMetricNotFound) {
		t.Fatalf("缺失指标应返回 ErrMetricNotFound，实际为 %v", err)
	}
}
