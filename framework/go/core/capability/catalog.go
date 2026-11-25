package capability

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	ErrInvalidCatalog       = errors.New("Framework 能力清单无效")
	ErrInvalidSelection     = errors.New("Framework 能力选择无效")
	ErrCapabilityNotFound   = errors.New("Framework 能力不存在")
	ErrMaturityInsufficient = errors.New("Framework 能力成熟度不足")

	capabilityIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)*$`)
	apiVersionPattern   = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
)

type Maturity string

const (
	MaturityPlanned               Maturity = "planned"
	MaturityContract              Maturity = "contract"
	MaturityImplemented           Maturity = "implemented"
	MaturityCrossPlatformVerified Maturity = "cross-platform-verified"
	MaturityProductionReady       Maturity = "production-ready"
)

type APIStability string

const (
	APIInternal     APIStability = "internal"
	APIExperimental APIStability = "experimental"
	APICandidate    APIStability = "candidate"
	APIStable       APIStability = "stable"
	APIDeprecated   APIStability = "deprecated"
)

type Definition struct {
	CapabilityID        string       `json:"capability_id"`
	Name                string       `json:"name"`
	Module              string       `json:"module"`
	Maturity            Maturity     `json:"maturity"`
	APIStability        APIStability `json:"api_stability"`
	Platforms           []string     `json:"platforms"`
	DependsOn           []string     `json:"depends_on"`
	DefaultEnabled      bool         `json:"default_enabled"`
	Degradation         string       `json:"degradation"`
	PublicContracts     []string     `json:"public_contracts"`
	ConfigSchemaRefs    []string     `json:"config_schema_refs"`
	RequiredPermissions []string     `json:"required_permissions"`
	AuditEvents         []string     `json:"audit_events"`
	Metrics             []string     `json:"metrics"`
	TestSuites          []string     `json:"test_suites"`
	Owner               string       `json:"owner"`
	VerifiedArtifact    *string      `json:"verified_artifact"`
	VerifiedAt          *string      `json:"verified_at"`
	KnownLimitations    []string     `json:"known_limitations"`
}

type Catalog struct {
	SchemaRef           string       `json:"$schema"`
	SchemaVersion       int          `json:"schema_version"`
	CatalogRevision     uint64       `json:"catalog_revision"`
	FrameworkAPIVersion string       `json:"framework_api_version"`
	Capabilities        []Definition `json:"capabilities"`

	byID map[string]Definition
}

type Selection struct {
	CatalogRevision     uint64
	FrameworkAPIVersion string
	Requested           []string
	Resolved            []string
}

// Digest 返回能力选择的确定性摘要，用于跨进程证明应用使用同一能力闭包。
func (selection Selection) Digest() (string, error) {
	if selection.CatalogRevision == 0 || !apiVersionPattern.MatchString(selection.FrameworkAPIVersion) ||
		len(selection.Requested) == 0 || len(selection.Resolved) == 0 ||
		hasDuplicates(selection.Requested) || hasDuplicates(selection.Resolved) {
		return "", ErrInvalidSelection
	}
	for _, values := range [][]string{selection.Requested, selection.Resolved} {
		for _, id := range values {
			if !capabilityIDPattern.MatchString(id) {
				return "", ErrInvalidSelection
			}
		}
	}
	canonical, err := json.Marshal(struct {
		CatalogRevision     uint64   `json:"catalog_revision"`
		FrameworkAPIVersion string   `json:"framework_api_version"`
		Requested           []string `json:"requested"`
		Resolved            []string `json:"resolved"`
	}{selection.CatalogRevision, selection.FrameworkAPIVersion, selection.Requested, selection.Resolved})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidSelection, err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
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

func (catalog Catalog) Lookup(id string) (Definition, bool) {
	definition, ok := catalog.byID[id]
	return definition, ok
}

func (catalog Catalog) Resolve(requested []string) (Selection, error) {
	if len(catalog.byID) == 0 {
		return Selection{}, fmt.Errorf("%w: Catalog 尚未校验", ErrInvalidCatalog)
	}
	unique := make([]string, 0, len(requested))
	seenRequested := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		if _, exists := seenRequested[id]; exists {
			continue
		}
		if _, exists := catalog.byID[id]; !exists {
			return Selection{}, fmt.Errorf("%w: %s", ErrCapabilityNotFound, id)
		}
		seenRequested[id] = struct{}{}
		unique = append(unique, id)
	}

	resolved := make([]string, 0, len(unique))
	visited := make(map[string]bool, len(catalog.byID))
	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		for _, dependency := range catalog.byID[id].DependsOn {
			visit(dependency)
		}
		visited[id] = true
		resolved = append(resolved, id)
	}
	for _, id := range unique {
		visit(id)
	}
	return Selection{
		CatalogRevision: catalog.CatalogRevision, FrameworkAPIVersion: catalog.FrameworkAPIVersion,
		Requested: unique, Resolved: resolved,
	}, nil
}

func (catalog Catalog) ValidateProduction(selection Selection) error {
	for _, id := range selection.Resolved {
		definition, exists := catalog.byID[id]
		if !exists {
			return fmt.Errorf("%w: %s", ErrCapabilityNotFound, id)
		}
		if maturityRank(definition.Maturity) < maturityRank(MaturityCrossPlatformVerified) {
			return fmt.Errorf("%w: %s=%s", ErrMaturityInsufficient, id, definition.Maturity)
		}
		if definition.VerifiedArtifact == nil || strings.TrimSpace(*definition.VerifiedArtifact) == "" || definition.VerifiedAt == nil {
			return fmt.Errorf("%w: %s 缺少验证制品或日期", ErrMaturityInsufficient, id)
		}
		if _, err := time.Parse(time.RFC3339, *definition.VerifiedAt); err != nil {
			return fmt.Errorf("%w: %s 验证日期无效", ErrMaturityInsufficient, id)
		}
	}
	return nil
}

func (catalog *Catalog) validate() error {
	if catalog.SchemaVersion != SchemaVersion || catalog.CatalogRevision == 0 || !apiVersionPattern.MatchString(catalog.FrameworkAPIVersion) || len(catalog.Capabilities) == 0 {
		return ErrInvalidCatalog
	}
	catalog.byID = make(map[string]Definition, len(catalog.Capabilities))
	for _, definition := range catalog.Capabilities {
		if err := validateDefinition(definition); err != nil {
			return err
		}
		if _, exists := catalog.byID[definition.CapabilityID]; exists {
			return fmt.Errorf("%w: 重复 capability_id %s", ErrInvalidCatalog, definition.CapabilityID)
		}
		catalog.byID[definition.CapabilityID] = definition
	}
	for _, definition := range catalog.Capabilities {
		for _, dependency := range definition.DependsOn {
			if _, exists := catalog.byID[dependency]; !exists || dependency == definition.CapabilityID {
				return fmt.Errorf("%w: %s 依赖无效的 %s", ErrInvalidCatalog, definition.CapabilityID, dependency)
			}
		}
	}
	if err := catalog.validateAcyclic(); err != nil {
		return err
	}
	return nil
}

func validateDefinition(definition Definition) error {
	validMaturity := maturityRank(definition.Maturity) >= 0
	validStability := map[APIStability]bool{APIInternal: true, APIExperimental: true, APICandidate: true, APIStable: true, APIDeprecated: true}[definition.APIStability]
	module := strings.Split(definition.CapabilityID, ".")[0]
	if !capabilityIDPattern.MatchString(definition.CapabilityID) || definition.Module != module || strings.TrimSpace(definition.Name) == "" ||
		!validMaturity || !validStability || len(definition.Platforms) == 0 || strings.TrimSpace(definition.Degradation) == "" ||
		len(definition.PublicContracts) == 0 || len(definition.TestSuites) == 0 || strings.TrimSpace(definition.Owner) == "" {
		return fmt.Errorf("%w: 能力 %q 字段不完整", ErrInvalidCatalog, definition.CapabilityID)
	}
	if hasDuplicates(definition.Platforms) || hasDuplicates(definition.DependsOn) {
		return fmt.Errorf("%w: 能力 %s 包含重复列表项", ErrInvalidCatalog, definition.CapabilityID)
	}
	allowedPlatforms := map[string]bool{"macos-arm64": true, "macos-x86_64": true, "windows-x64": true}
	for _, platform := range definition.Platforms {
		if !allowedPlatforms[platform] {
			return fmt.Errorf("%w: 能力 %s 平台无效", ErrInvalidCatalog, definition.CapabilityID)
		}
	}
	if definition.DefaultEnabled && maturityRank(definition.Maturity) < maturityRank(MaturityCrossPlatformVerified) {
		return fmt.Errorf("%w: 未验证能力 %s 不得默认启用", ErrInvalidCatalog, definition.CapabilityID)
	}
	verified := maturityRank(definition.Maturity) >= maturityRank(MaturityCrossPlatformVerified)
	if verified != (definition.VerifiedArtifact != nil && definition.VerifiedAt != nil) {
		return fmt.Errorf("%w: 能力 %s 验证证据与成熟度不一致", ErrInvalidCatalog, definition.CapabilityID)
	}
	if verified {
		if strings.TrimSpace(*definition.VerifiedArtifact) == "" || strings.TrimSpace(*definition.VerifiedAt) == "" {
			return fmt.Errorf("%w: 能力 %s 验证证据为空", ErrInvalidCatalog, definition.CapabilityID)
		}
		if _, err := time.Parse(time.RFC3339, *definition.VerifiedAt); err != nil {
			return fmt.Errorf("%w: 能力 %s 验证日期无效", ErrInvalidCatalog, definition.CapabilityID)
		}
	}
	return nil
}

func (catalog Catalog) validateAcyclic() error {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(catalog.byID))
	ids := make([]string, 0, len(catalog.byID))
	for id := range catalog.byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("%w: 能力依赖存在环 %s", ErrInvalidCatalog, id)
		case visited:
			return nil
		}
		state[id] = visiting
		for _, dependency := range catalog.byID[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: JSON 包含额外内容", ErrInvalidCatalog)
	}
	return nil
}

func maturityRank(value Maturity) int {
	switch value {
	case MaturityPlanned:
		return 0
	case MaturityContract:
		return 1
	case MaturityImplemented:
		return 2
	case MaturityCrossPlatformVerified:
		return 3
	case MaturityProductionReady:
		return 4
	default:
		return -1
	}
}

func hasDuplicates(values []string) bool {
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
