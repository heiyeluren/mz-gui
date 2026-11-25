package capability

import (
	"errors"
	"testing"
)

func TestBuildEvidenceReportLinksCapabilitiesToRequirements(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildEvidenceReport(catalog, []EvidenceRequirement{
		{
			RequirementID:        "FW-RUNTIME-001",
			ImplementationStatus: "partial",
			ImplementationRefs:   []string{"framework/go/runtime/taskengine", "framework/go/runtime/scheduler"},
			ContractRefs:         []string{"framework/go/runtime/taskengine/engine.go"},
			TestRefs:             []string{"framework/go/runtime/scheduler/engine_test.go"},
			EvidenceRefs:         []string{"docs/testing/stage-runtime.md"},
			Gaps:                 []string{"缺少真机矩阵"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 3 || report.Linked != 2 || report.Unlinked != 1 {
		t.Fatalf("聚合统计错误: %#v", report)
	}
	scheduler := findCapabilityEvidence(report, "runtime.scheduler")
	if scheduler == nil || len(scheduler.Requirements) != 1 || scheduler.Requirements[0].RequirementID != "FW-RUNTIME-001" {
		t.Fatalf("scheduler 证据关联错误: %#v", scheduler)
	}
	if !containsString(scheduler.MissingEvidenceRefs, "verified-artifact") {
		t.Fatalf("未验证能力必须标记 verified-artifact 缺口: %#v", scheduler.MissingEvidenceRefs)
	}
	unlinked := findCapabilityEvidence(report, "storage.sqlite")
	if unlinked == nil || !containsString(unlinked.MissingEvidenceRefs, "requirement-link") || !containsString(unlinked.MissingEvidenceRefs, "requirement-evidence") {
		t.Fatalf("未关联能力必须标记需求和证据缺口: %#v", unlinked)
	}
}

func TestBuildEvidenceReportRejectsInvalidInputs(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildEvidenceReport(Catalog{}, nil); !errors.Is(err, ErrInvalidCatalog) {
		t.Fatalf("未校验 Catalog 应失败，实际为 %v", err)
	}
	_, err = BuildEvidenceReport(catalog, []EvidenceRequirement{
		{RequirementID: "FW-RUNTIME-001"},
		{RequirementID: "FW-RUNTIME-001"},
	})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("重复 requirement_id 应失败，实际为 %v", err)
	}
}

func TestBuildEvidenceReportDoesNotLinkBroadArchitectureRefs(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	report, err := BuildEvidenceReport(catalog, []EvidenceRequirement{
		{
			RequirementID:      "FW-ARCH-001",
			ImplementationRefs: []string{"framework/go"},
			EvidenceRefs:       []string{"docs/testing/arch.md"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Linked != 0 || report.Unlinked != 3 {
		t.Fatalf("过宽架构引用不应关联具体能力: %#v", report)
	}
}

func findCapabilityEvidence(report EvidenceReport, id string) *CapabilityEvidence {
	for i := range report.Capabilities {
		if report.Capabilities[i].CapabilityID == id {
			return &report.Capabilities[i]
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
