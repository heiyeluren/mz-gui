package requirements

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var (
	ErrInvalidCatalog      = errors.New("Framework 需求矩阵无效")
	ErrInvalidReference    = errors.New("Framework 需求引用无效")
	ErrApplicationCoupling = errors.New("Framework 需求反向引用应用实现")

	requirementIDPattern = regexp.MustCompile(`^FW-[A-Z]+-[0-9]{3}$`)
	releasePattern       = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
	domainPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

type ImplementationStatus string

const (
	StatusImplemented ImplementationStatus = "implemented"
	StatusPartial     ImplementationStatus = "partial"
	StatusMissing     ImplementationStatus = "missing"
	StatusDeferred    ImplementationStatus = "deferred"
)

type VerificationStatus string

const (
	VerificationLocalAutomated VerificationStatus = "local-automated"
	VerificationCrossCompiled  VerificationStatus = "cross-compiled"
	VerificationNotVerified    VerificationStatus = "not-verified"
	VerificationNotApplicable  VerificationStatus = "not-applicable"
)

var platforms = []string{"macos-arm64", "macos-x86_64", "windows-x64"}

type Requirement struct {
	RequirementID        string                        `json:"requirement_id"`
	Phase                string                        `json:"phase"`
	Domain               string                        `json:"domain"`
	Title                string                        `json:"title"`
	Priority             string                        `json:"priority"`
	ImplementationStatus ImplementationStatus          `json:"implementation_status"`
	DesignRefs           []string                      `json:"design_refs"`
	ImplementationRefs   []string                      `json:"implementation_refs"`
	ContractRefs         []string                      `json:"contract_refs"`
	TestRefs             []string                      `json:"test_refs"`
	EvidenceRefs         []string                      `json:"evidence_refs"`
	Gaps                 []string                      `json:"gaps"`
	PlatformVerification map[string]VerificationStatus `json:"platform_verification"`
}

type Catalog struct {
	SchemaRef     string        `json:"$schema,omitempty"`
	SchemaVersion int           `json:"schema_version"`
	Release       string        `json:"release"`
	Requirements  []Requirement `json:"requirements"`
}

type PlatformSummary struct {
	LocalAutomated int `json:"local_automated"`
	CrossCompiled  int `json:"cross_compiled"`
	NotVerified    int `json:"not_verified"`
	NotApplicable  int `json:"not_applicable"`
}

type Summary struct {
	Release     string                     `json:"release"`
	Total       int                        `json:"total"`
	Implemented int                        `json:"implemented"`
	Partial     int                        `json:"partial"`
	Missing     int                        `json:"missing"`
	Deferred    int                        `json:"deferred"`
	Platforms   map[string]PlatformSummary `json:"platforms"`
}

func Decode(data []byte) (Catalog, error) {
	var catalog Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Catalog{}, err
	}
	if err := catalog.validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (catalog Catalog) IDs() []string {
	ids := make([]string, 0, len(catalog.Requirements))
	for _, requirement := range catalog.Requirements {
		ids = append(ids, requirement.RequirementID)
	}
	return ids
}

func (catalog Catalog) Summary() Summary {
	summary := Summary{
		Release:   catalog.Release,
		Total:     len(catalog.Requirements),
		Platforms: make(map[string]PlatformSummary, len(platforms)),
	}
	for _, requirement := range catalog.Requirements {
		switch requirement.ImplementationStatus {
		case StatusImplemented:
			summary.Implemented++
		case StatusPartial:
			summary.Partial++
		case StatusMissing:
			summary.Missing++
		case StatusDeferred:
			summary.Deferred++
		}
		for _, platform := range platforms {
			platformSummary := summary.Platforms[platform]
			switch requirement.PlatformVerification[platform] {
			case VerificationLocalAutomated:
				platformSummary.LocalAutomated++
			case VerificationCrossCompiled:
				platformSummary.CrossCompiled++
			case VerificationNotVerified:
				platformSummary.NotVerified++
			case VerificationNotApplicable:
				platformSummary.NotApplicable++
			}
			summary.Platforms[platform] = platformSummary
		}
	}
	return summary
}

func (catalog Catalog) validate() error {
	if catalog.SchemaVersion != SchemaVersion || !releasePattern.MatchString(catalog.Release) || len(catalog.Requirements) == 0 {
		return fmt.Errorf("%w: 顶层字段无效", ErrInvalidCatalog)
	}
	seen := make(map[string]struct{}, len(catalog.Requirements))
	for _, requirement := range catalog.Requirements {
		if err := validateRequirement(requirement); err != nil {
			return err
		}
		if _, exists := seen[requirement.RequirementID]; exists {
			return fmt.Errorf("%w: 重复需求 %s", ErrInvalidCatalog, requirement.RequirementID)
		}
		seen[requirement.RequirementID] = struct{}{}
	}
	return nil
}

func validateRequirement(requirement Requirement) error {
	if !requirementIDPattern.MatchString(requirement.RequirementID) ||
		!validPhase(requirement.Phase) || !domainPattern.MatchString(requirement.Domain) ||
		strings.TrimSpace(requirement.Title) == "" || !validPriority(requirement.Priority) ||
		!validImplementationStatus(requirement.ImplementationStatus) || len(requirement.DesignRefs) == 0 {
		return fmt.Errorf("%w: 需求 %q 字段不完整", ErrInvalidCatalog, requirement.RequirementID)
	}
	for _, values := range [][]string{
		requirement.DesignRefs,
		requirement.ImplementationRefs,
		requirement.ContractRefs,
		requirement.TestRefs,
		requirement.EvidenceRefs,
		requirement.Gaps,
	} {
		if hasBlankOrDuplicate(values) {
			return fmt.Errorf("%w: 需求 %s 包含空值或重复项", ErrInvalidCatalog, requirement.RequirementID)
		}
	}
	if len(requirement.PlatformVerification) != len(platforms) {
		return fmt.Errorf("%w: 需求 %s 平台集合不完整", ErrInvalidCatalog, requirement.RequirementID)
	}
	for _, platform := range platforms {
		status, exists := requirement.PlatformVerification[platform]
		if !exists || !validVerificationStatus(status) {
			return fmt.Errorf("%w: 需求 %s 的平台 %s 状态无效", ErrInvalidCatalog, requirement.RequirementID, platform)
		}
	}
	for platform := range requirement.PlatformVerification {
		if !contains(platforms, platform) {
			return fmt.Errorf("%w: 需求 %s 包含未知平台 %s", ErrInvalidCatalog, requirement.RequirementID, platform)
		}
	}
	switch requirement.ImplementationStatus {
	case StatusImplemented:
		if len(requirement.ImplementationRefs) == 0 || len(requirement.TestRefs) == 0 {
			return fmt.Errorf("%w: 已实现需求 %s 缺少实现或测试", ErrInvalidCatalog, requirement.RequirementID)
		}
	case StatusPartial:
		if len(requirement.ImplementationRefs) == 0 || len(requirement.Gaps) == 0 {
			return fmt.Errorf("%w: 部分实现需求 %s 缺少实现或缺口", ErrInvalidCatalog, requirement.RequirementID)
		}
	case StatusMissing:
		if len(requirement.ImplementationRefs) != 0 || len(requirement.ContractRefs) != 0 || len(requirement.Gaps) == 0 {
			return fmt.Errorf("%w: 未实现需求 %s 状态与引用矛盾", ErrInvalidCatalog, requirement.RequirementID)
		}
	case StatusDeferred:
		if len(requirement.Gaps) == 0 {
			return fmt.Errorf("%w: 延后需求 %s 必须说明边界", ErrInvalidCatalog, requirement.RequirementID)
		}
	}
	return nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: 存在多个 JSON 值", ErrInvalidCatalog)
		}
		return fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	return nil
}

func validPhase(value string) bool {
	switch value {
	case "F0", "F1", "F2", "F3", "F4", "F5", "F6":
		return true
	default:
		return false
	}
}

func validPriority(value string) bool {
	return value == "P0" || value == "P1" || value == "P2"
}

func validImplementationStatus(value ImplementationStatus) bool {
	switch value {
	case StatusImplemented, StatusPartial, StatusMissing, StatusDeferred:
		return true
	default:
		return false
	}
}

func validVerificationStatus(value VerificationStatus) bool {
	switch value {
	case VerificationLocalAutomated, VerificationCrossCompiled, VerificationNotVerified, VerificationNotApplicable:
		return true
	default:
		return false
	}
}

func hasBlankOrDuplicate(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
