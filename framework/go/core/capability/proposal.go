package capability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ProposalSchemaVersion = 1

var ErrInvalidProposal = errors.New("Framework 能力提案无效")

type Proposal struct {
	SchemaVersion            int                 `json:"schema_version"`
	ProposalID               string              `json:"proposal_id"`
	ChangeType               string              `json:"change_type"`
	Definition               Definition          `json:"definition"`
	NewDefinitions           []Definition        `json:"new_definitions"`
	AffectedCapabilityIDs    []string            `json:"affected_capability_ids"`
	ReplacementCapabilityIDs []string            `json:"replacement_capability_ids"`
	CompatibilityWindow      CompatibilityWindow `json:"compatibility_window"`
	Rationale                string              `json:"rationale"`
	ADRRefs                  []string            `json:"adr_refs"`
	EvidenceRefs             []string            `json:"evidence_refs"`
	MigrationNotes           string              `json:"migration_notes"`
}

type CompatibilityWindow struct {
	FromVersion        string `json:"from_version"`
	RemoveAfterVersion string `json:"remove_after_version"`
	MigrationGuide     string `json:"migration_guide"`
}

type ProposalReview struct {
	ProposalID               string   `json:"proposal_id"`
	ChangeType               string   `json:"change_type"`
	CapabilityID             string   `json:"capability_id"`
	AffectedCapabilityIDs    []string `json:"affected_capability_ids"`
	ReplacementCapabilityIDs []string `json:"replacement_capability_ids"`
	Accepted                 bool     `json:"accepted"`
	RequiredRefs             []string `json:"required_refs"`
	KnownGaps                []string `json:"known_gaps"`
}

func DecodeProposal(data []byte) (Proposal, error) {
	var proposal Proposal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalidProposal, err)
	}
	return proposal, nil
}

func ReviewProposal(catalog Catalog, proposal Proposal) (ProposalReview, error) {
	review := ProposalReview{
		ProposalID:               strings.TrimSpace(proposal.ProposalID),
		ChangeType:               strings.TrimSpace(proposal.ChangeType),
		CapabilityID:             strings.TrimSpace(proposal.Definition.CapabilityID),
		AffectedCapabilityIDs:    nonEmpty(proposal.AffectedCapabilityIDs),
		ReplacementCapabilityIDs: nonEmpty(proposal.ReplacementCapabilityIDs),
	}
	if proposal.SchemaVersion != ProposalSchemaVersion || review.ProposalID == "" || !validProposalChangeType(review.ChangeType) {
		return review, ErrInvalidProposal
	}
	if len(catalog.byID) == 0 {
		return review, fmt.Errorf("%w: Catalog 尚未校验", ErrInvalidCatalog)
	}
	if strings.TrimSpace(proposal.Rationale) == "" || strings.TrimSpace(proposal.MigrationNotes) == "" || len(nonEmpty(proposal.ADRRefs)) == 0 {
		return review, ErrInvalidProposal
	}
	switch review.ChangeType {
	case "add":
		if err := reviewAddProposal(catalog, proposal, &review); err != nil {
			return review, err
		}
	case "rename", "split":
		if err := reviewStructuralMigrationProposal(catalog, proposal, &review); err != nil {
			return review, err
		}
	case "deprecate":
		if err := reviewDeprecationProposal(catalog, proposal, &review); err != nil {
			return review, err
		}
	}
	review.RequiredRefs = append([]string{}, proposal.ADRRefs...)
	for _, definition := range proposalDefinitions(proposal) {
		review.RequiredRefs = append(review.RequiredRefs, definition.PublicContracts...)
		review.RequiredRefs = append(review.RequiredRefs, definition.TestSuites...)
		review.KnownGaps = append(review.KnownGaps, definition.KnownLimitations...)
	}
	if proposal.CompatibilityWindow.MigrationGuide != "" {
		review.RequiredRefs = append(review.RequiredRefs, proposal.CompatibilityWindow.MigrationGuide)
	}
	review.Accepted = true
	return review, nil
}

func reviewAddProposal(catalog Catalog, proposal Proposal, review *ProposalReview) error {
	if len(nonEmpty(proposal.AffectedCapabilityIDs)) != 0 || len(nonEmpty(proposal.ReplacementCapabilityIDs)) != 0 || !emptyCompatibilityWindow(proposal.CompatibilityWindow) {
		return ErrInvalidProposal
	}
	definitions := proposalDefinitions(proposal)
	if len(definitions) != 1 {
		return ErrInvalidProposal
	}
	review.CapabilityID = definitions[0].CapabilityID
	return validateNewDefinitions(catalog, definitions)
}

func reviewStructuralMigrationProposal(catalog Catalog, proposal Proposal, review *ProposalReview) error {
	affected := nonEmpty(proposal.AffectedCapabilityIDs)
	definitions := proposalDefinitions(proposal)
	if len(affected) != 1 || hasDuplicates(affected) || !validCompatibilityWindow(proposal.CompatibilityWindow) {
		return ErrInvalidProposal
	}
	if review.ChangeType == "rename" && len(definitions) != 1 {
		return ErrInvalidProposal
	}
	if review.ChangeType == "split" && len(definitions) < 2 {
		return ErrInvalidProposal
	}
	if err := requireExistingCapabilities(catalog, affected); err != nil {
		return err
	}
	if err := validateNewDefinitions(catalog, definitions); err != nil {
		return err
	}
	replacements := definitionIDs(definitions)
	if !sameStringSet(replacements, nonEmpty(proposal.ReplacementCapabilityIDs)) {
		return ErrInvalidProposal
	}
	review.CapabilityID = strings.Join(replacements, ",")
	return nil
}

func reviewDeprecationProposal(catalog Catalog, proposal Proposal, review *ProposalReview) error {
	affected := nonEmpty(proposal.AffectedCapabilityIDs)
	replacements := nonEmpty(proposal.ReplacementCapabilityIDs)
	if len(affected) == 0 || hasDuplicates(affected) || len(replacements) == 0 || hasDuplicates(replacements) ||
		len(proposalDefinitions(proposal)) != 0 || !validCompatibilityWindow(proposal.CompatibilityWindow) {
		return ErrInvalidProposal
	}
	if err := requireExistingCapabilities(catalog, affected); err != nil {
		return err
	}
	if err := requireExistingCapabilities(catalog, replacements); err != nil {
		return err
	}
	review.CapabilityID = strings.Join(affected, ",")
	return nil
}

func validateNewDefinitions(catalog Catalog, definitions []Definition) error {
	if len(definitions) == 0 || hasDuplicateDefinitions(definitions) {
		return ErrInvalidProposal
	}
	for _, definition := range definitions {
		if _, exists := catalog.byID[definition.CapabilityID]; exists {
			return fmt.Errorf("%w: capability 已存在 %s", ErrInvalidProposal, definition.CapabilityID)
		}
		if err := validateDefinition(definition); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidProposal, err)
		}
		for _, dependency := range definition.DependsOn {
			if _, exists := catalog.byID[dependency]; !exists && !containsDefinitionID(definitions, dependency) {
				return fmt.Errorf("%w: 依赖不存在 %s", ErrInvalidProposal, dependency)
			}
		}
	}
	return nil
}

func requireExistingCapabilities(catalog Catalog, ids []string) error {
	for _, id := range ids {
		if _, exists := catalog.byID[id]; !exists {
			return fmt.Errorf("%w: capability 不存在 %s", ErrInvalidProposal, id)
		}
	}
	return nil
}

func proposalDefinitions(proposal Proposal) []Definition {
	if len(proposal.NewDefinitions) > 0 {
		return append([]Definition{}, proposal.NewDefinitions...)
	}
	if strings.TrimSpace(proposal.Definition.CapabilityID) == "" {
		return nil
	}
	return []Definition{proposal.Definition}
}

func definitionIDs(definitions []Definition) []string {
	ids := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ids = append(ids, strings.TrimSpace(definition.CapabilityID))
	}
	return ids
}

func hasDuplicateDefinitions(definitions []Definition) bool {
	return hasDuplicates(definitionIDs(definitions))
}

func containsDefinitionID(definitions []Definition, id string) bool {
	for _, definition := range definitions {
		if definition.CapabilityID == id {
			return true
		}
	}
	return false
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func validProposalChangeType(value string) bool {
	switch value {
	case "add", "rename", "split", "deprecate":
		return true
	default:
		return false
	}
}

func emptyCompatibilityWindow(window CompatibilityWindow) bool {
	return strings.TrimSpace(window.FromVersion) == "" && strings.TrimSpace(window.RemoveAfterVersion) == "" && strings.TrimSpace(window.MigrationGuide) == ""
}

func validCompatibilityWindow(window CompatibilityWindow) bool {
	return apiVersionPattern.MatchString(window.FromVersion) &&
		apiVersionPattern.MatchString(window.RemoveAfterVersion) &&
		strings.TrimSpace(window.MigrationGuide) != "" &&
		window.FromVersion != window.RemoveAfterVersion
}

func nonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, strings.TrimSpace(value))
		}
	}
	return result
}
