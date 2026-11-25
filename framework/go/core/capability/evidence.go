package capability

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidEvidence = errors.New("Framework 能力证据无效")

type EvidenceRequirement struct {
	RequirementID        string   `json:"requirement_id"`
	ImplementationStatus string   `json:"implementation_status"`
	ImplementationRefs   []string `json:"implementation_refs"`
	ContractRefs         []string `json:"contract_refs"`
	TestRefs             []string `json:"test_refs"`
	EvidenceRefs         []string `json:"evidence_refs"`
	Gaps                 []string `json:"gaps"`
}

type EvidenceReport struct {
	CatalogRevision     uint64               `json:"catalog_revision"`
	FrameworkAPIVersion string               `json:"framework_api_version"`
	Total               int                  `json:"total"`
	Linked              int                  `json:"linked"`
	Unlinked            int                  `json:"unlinked"`
	Capabilities        []CapabilityEvidence `json:"capabilities"`
}

type CapabilityEvidence struct {
	CapabilityID        string                `json:"capability_id"`
	Module              string                `json:"module"`
	Maturity            Maturity              `json:"maturity"`
	APIStability        APIStability          `json:"api_stability"`
	Owner               string                `json:"owner"`
	PublicContracts     []string              `json:"public_contracts"`
	TestSuites          []string              `json:"test_suites"`
	VerifiedArtifact    *string               `json:"verified_artifact"`
	VerifiedAt          *string               `json:"verified_at"`
	KnownLimitations    []string              `json:"known_limitations"`
	Requirements        []EvidenceRequirement `json:"requirements"`
	MissingEvidenceRefs []string              `json:"missing_evidence_refs"`
}

func BuildEvidenceReport(catalog Catalog, requirements []EvidenceRequirement) (EvidenceReport, error) {
	if len(catalog.byID) == 0 {
		return EvidenceReport{}, fmt.Errorf("%w: Catalog 尚未校验", ErrInvalidCatalog)
	}
	if hasDuplicateRequirementIDs(requirements) {
		return EvidenceReport{}, fmt.Errorf("%w: requirement_id 重复", ErrInvalidEvidence)
	}
	report := EvidenceReport{
		CatalogRevision:     catalog.CatalogRevision,
		FrameworkAPIVersion: catalog.FrameworkAPIVersion,
		Total:               len(catalog.Capabilities),
		Capabilities:        make([]CapabilityEvidence, 0, len(catalog.Capabilities)),
	}
	for _, definition := range catalog.Capabilities {
		evidence := CapabilityEvidence{
			CapabilityID:     definition.CapabilityID,
			Module:           definition.Module,
			Maturity:         definition.Maturity,
			APIStability:     definition.APIStability,
			Owner:            definition.Owner,
			PublicContracts:  append([]string{}, definition.PublicContracts...),
			TestSuites:       append([]string{}, definition.TestSuites...),
			VerifiedArtifact: definition.VerifiedArtifact,
			VerifiedAt:       definition.VerifiedAt,
			KnownLimitations: append([]string{}, definition.KnownLimitations...),
			Requirements:     matchingRequirements(definition, requirements),
		}
		evidence.MissingEvidenceRefs = missingEvidenceRefs(evidence)
		if len(evidence.Requirements) == 0 {
			report.Unlinked++
		} else {
			report.Linked++
		}
		report.Capabilities = append(report.Capabilities, evidence)
	}
	sort.SliceStable(report.Capabilities, func(i, j int) bool {
		return report.Capabilities[i].CapabilityID < report.Capabilities[j].CapabilityID
	})
	return report, nil
}

func matchingRequirements(definition Definition, requirements []EvidenceRequirement) []EvidenceRequirement {
	matches := make([]EvidenceRequirement, 0)
	for _, requirement := range requirements {
		refs := append([]string{}, requirement.ImplementationRefs...)
		refs = append(refs, requirement.ContractRefs...)
		refs = append(refs, requirement.TestRefs...)
		if referencesCapability(definition, refs) {
			matches = append(matches, copyEvidenceRequirement(requirement))
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].RequirementID < matches[j].RequirementID
	})
	return matches
}

func referencesCapability(definition Definition, refs []string) bool {
	for _, contract := range definition.PublicContracts {
		if strings.TrimSpace(contract) == "" {
			continue
		}
		for _, ref := range refs {
			if ref == contract || strings.HasPrefix(ref, contract+"/") || (isSpecificParentRef(ref) && strings.HasPrefix(contract, ref+"/")) {
				return true
			}
		}
	}
	return false
}

func isSpecificParentRef(ref string) bool {
	ref = strings.Trim(ref, "/")
	if strings.HasSuffix(ref, ".go") || strings.HasSuffix(ref, ".json") || strings.HasSuffix(ref, ".schema.json") {
		return true
	}
	return len(strings.Split(ref, "/")) >= 4
}

func missingEvidenceRefs(evidence CapabilityEvidence) []string {
	missing := make([]string, 0)
	if len(evidence.Requirements) == 0 {
		missing = append(missing, "requirement-link")
	}
	hasEvidence := false
	for _, requirement := range evidence.Requirements {
		if len(requirement.EvidenceRefs) > 0 {
			hasEvidence = true
			break
		}
	}
	if !hasEvidence {
		missing = append(missing, "requirement-evidence")
	}
	if evidence.VerifiedArtifact == nil || strings.TrimSpace(*evidence.VerifiedArtifact) == "" || evidence.VerifiedAt == nil || strings.TrimSpace(*evidence.VerifiedAt) == "" {
		missing = append(missing, "verified-artifact")
	}
	return missing
}

func hasDuplicateRequirementIDs(requirements []EvidenceRequirement) bool {
	seen := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		id := strings.TrimSpace(requirement.RequirementID)
		if id == "" {
			return true
		}
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}

func copyEvidenceRequirement(requirement EvidenceRequirement) EvidenceRequirement {
	return EvidenceRequirement{
		RequirementID:        requirement.RequirementID,
		ImplementationStatus: requirement.ImplementationStatus,
		ImplementationRefs:   append([]string{}, requirement.ImplementationRefs...),
		ContractRefs:         append([]string{}, requirement.ContractRefs...),
		TestRefs:             append([]string{}, requirement.TestRefs...),
		EvidenceRefs:         append([]string{}, requirement.EvidenceRefs...),
		Gaps:                 append([]string{}, requirement.Gaps...),
	}
}
