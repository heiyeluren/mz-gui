package capability

import (
	"errors"
	"strings"
	"testing"
)

func TestReviewProposalAcceptsCompleteAdd(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	proposal := validProposal()
	review, err := ReviewProposal(catalog, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Accepted || review.CapabilityID != "observability.trace" || len(review.RequiredRefs) == 0 {
		t.Fatalf("提案评审结果错误: %#v", review)
	}
}

func TestReviewProposalRejectsDuplicateAndUnknownDependency(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := validProposal()
	duplicate.Definition.CapabilityID = "runtime.task"
	duplicate.Definition.Module = "runtime"
	if _, err := ReviewProposal(catalog, duplicate); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("重复 capability 应失败，实际为 %v", err)
	}
	missingDependency := validProposal()
	missingDependency.Definition.DependsOn = []string{"storage.missing"}
	if _, err := ReviewProposal(catalog, missingDependency); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("未知依赖应失败，实际为 %v", err)
	}
}

func TestReviewProposalAcceptsRenameSplitAndDeprecate(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	rename := validProposal()
	rename.ChangeType = "rename"
	rename.AffectedCapabilityIDs = []string{"runtime.scheduler"}
	rename.ReplacementCapabilityIDs = []string{"runtime.plan-scheduler"}
	rename.CompatibilityWindow = validTestCompatibilityWindow()
	rename.Definition = validDefinition("runtime.plan-scheduler", "Plan Scheduler")
	review, err := ReviewProposal(catalog, rename)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Accepted || review.ChangeType != "rename" || review.CapabilityID != "runtime.plan-scheduler" {
		t.Fatalf("rename proposal 评审错误: %#v", review)
	}

	split := validProposal()
	split.ChangeType = "split"
	split.Definition = Definition{}
	split.AffectedCapabilityIDs = []string{"runtime.scheduler"}
	split.ReplacementCapabilityIDs = []string{"runtime.plan-scheduler", "runtime.delay-queue"}
	split.CompatibilityWindow = validTestCompatibilityWindow()
	first := validDefinition("runtime.plan-scheduler", "Plan Scheduler")
	second := validDefinition("runtime.delay-queue", "Delay Queue")
	second.DependsOn = []string{"runtime.plan-scheduler"}
	split.NewDefinitions = []Definition{first, second}
	review, err = ReviewProposal(catalog, split)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Accepted || review.ChangeType != "split" || review.CapabilityID != "runtime.plan-scheduler,runtime.delay-queue" {
		t.Fatalf("split proposal 评审错误: %#v", review)
	}

	deprecate := validProposal()
	deprecate.ChangeType = "deprecate"
	deprecate.Definition = Definition{}
	deprecate.AffectedCapabilityIDs = []string{"runtime.scheduler"}
	deprecate.ReplacementCapabilityIDs = []string{"runtime.task"}
	deprecate.CompatibilityWindow = validTestCompatibilityWindow()
	review, err = ReviewProposal(catalog, deprecate)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Accepted || review.ChangeType != "deprecate" || review.CapabilityID != "runtime.scheduler" {
		t.Fatalf("deprecate proposal 评审错误: %#v", review)
	}
}

func TestReviewProposalRejectsInvalidMigrationWindows(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	tests := []Proposal{
		func() Proposal {
			p := validProposal()
			p.ChangeType = "rename"
			p.AffectedCapabilityIDs = []string{"runtime.scheduler"}
			p.ReplacementCapabilityIDs = []string{"runtime.plan-scheduler"}
			p.Definition = validDefinition("runtime.plan-scheduler", "Plan Scheduler")
			return p
		}(),
		func() Proposal {
			p := validProposal()
			p.ChangeType = "rename"
			p.AffectedCapabilityIDs = []string{"runtime.missing"}
			p.ReplacementCapabilityIDs = []string{"runtime.plan-scheduler"}
			p.CompatibilityWindow = validTestCompatibilityWindow()
			p.Definition = validDefinition("runtime.plan-scheduler", "Plan Scheduler")
			return p
		}(),
		func() Proposal {
			p := validProposal()
			p.ChangeType = "rename"
			p.AffectedCapabilityIDs = []string{"runtime.scheduler"}
			p.ReplacementCapabilityIDs = []string{"runtime.other"}
			p.CompatibilityWindow = validTestCompatibilityWindow()
			p.Definition = validDefinition("runtime.plan-scheduler", "Plan Scheduler")
			return p
		}(),
		func() Proposal {
			p := validProposal()
			p.ChangeType = "deprecate"
			p.Definition = Definition{}
			p.AffectedCapabilityIDs = []string{"runtime.scheduler"}
			p.ReplacementCapabilityIDs = []string{"runtime.missing"}
			p.CompatibilityWindow = validTestCompatibilityWindow()
			return p
		}(),
	}
	for _, proposal := range tests {
		if _, err := ReviewProposal(catalog, proposal); !errors.Is(err, ErrInvalidProposal) {
			t.Fatalf("非法迁移 proposal 应失败: %#v err=%v", proposal, err)
		}
	}
}

func TestReviewProposalRejectsMissingGovernanceFields(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	tests := []Proposal{
		func() Proposal { p := validProposal(); p.SchemaVersion = 0; return p }(),
		func() Proposal { p := validProposal(); p.ChangeType = "rename"; return p }(),
		func() Proposal { p := validProposal(); p.Rationale = ""; return p }(),
		func() Proposal { p := validProposal(); p.ADRRefs = nil; return p }(),
		func() Proposal { p := validProposal(); p.MigrationNotes = " "; return p }(),
		func() Proposal { p := validProposal(); p.Definition.PublicContracts = nil; return p }(),
		func() Proposal { p := validProposal(); p.Definition.TestSuites = nil; return p }(),
	}
	for _, proposal := range tests {
		if _, err := ReviewProposal(catalog, proposal); !errors.Is(err, ErrInvalidProposal) {
			t.Fatalf("非法 proposal 应失败: %#v err=%v", proposal, err)
		}
	}
}

func TestDecodeProposalRejectsUnknownAndTrailingFields(t *testing.T) {
	data := `{"schema_version":1,"proposal_id":"p","change_type":"add","definition":{},"rationale":"r","adr_refs":[],"evidence_refs":[],"migration_notes":"m","unexpected":true}`
	if _, err := DecodeProposal([]byte(data)); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("未知字段应失败，实际为 %v", err)
	}
	if _, err := DecodeProposal([]byte(strings.Replace(data, `,"unexpected":true`, `} {`, 1))); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("尾随 JSON 应失败，实际为 %v", err)
	}
}

func validProposal() Proposal {
	return Proposal{
		SchemaVersion:  ProposalSchemaVersion,
		ProposalID:     "20260815-observability-trace",
		ChangeType:     "add",
		Rationale:      "为跨进程链路提供统一 trace context。",
		ADRRefs:        []string{"docs/adr/0060-framework-observability-trace-metrics.md"},
		EvidenceRefs:   []string{"docs/testing/stage-72-framework-observability-trace-metrics-validation.md"},
		MigrationNotes: "新增能力默认关闭，由应用 manifest 显式声明。",
		Definition: Definition{
			CapabilityID:        "observability.trace",
			Name:                "Trace Context",
			Module:              "observability",
			Maturity:            MaturityImplemented,
			APIStability:        APICandidate,
			Platforms:           []string{"macos-arm64", "windows-x64"},
			DependsOn:           []string{"runtime.task"},
			DefaultEnabled:      false,
			Degradation:         "new-root-trace",
			PublicContracts:     []string{"framework/go/observability/tracecontext"},
			ConfigSchemaRefs:    []string{},
			RequiredPermissions: []string{},
			AuditEvents:         []string{},
			Metrics:             []string{},
			TestSuites:          []string{"framework-tracecontext"},
			Owner:               "framework-observability",
			VerifiedArtifact:    nil,
			VerifiedAt:          nil,
			KnownLimitations:    []string{"middleware 尚未接入"},
		},
	}
}

func validDefinition(id, name string) Definition {
	return Definition{
		CapabilityID:        id,
		Name:                name,
		Module:              "runtime",
		Maturity:            MaturityImplemented,
		APIStability:        APICandidate,
		Platforms:           []string{"macos-arm64", "windows-x64"},
		DependsOn:           []string{"runtime.task"},
		DefaultEnabled:      false,
		Degradation:         "disable-entry",
		PublicContracts:     []string{"framework/go/runtime/" + strings.ReplaceAll(strings.TrimPrefix(id, "runtime."), "-", "")},
		ConfigSchemaRefs:    []string{},
		RequiredPermissions: []string{},
		AuditEvents:         []string{},
		Metrics:             []string{},
		TestSuites:          []string{"framework-" + strings.TrimPrefix(id, "runtime.")},
		Owner:               "framework-runtime",
		VerifiedArtifact:    nil,
		VerifiedAt:          nil,
		KnownLimitations:    []string{"迁移窗口尚未关闭"},
	}
}

func validTestCompatibilityWindow() CompatibilityWindow {
	return CompatibilityWindow{
		FromVersion:        "1.0.0",
		RemoveAfterVersion: "2.0.0",
		MigrationGuide:     "docs/adr/0064-framework-capability-migration-window.md",
	}
}
