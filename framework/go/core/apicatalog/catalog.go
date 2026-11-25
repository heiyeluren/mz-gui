// Package apicatalog 管理 Framework 公共 API 清单与兼容性检查。
package apicatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const SchemaVersion = 1

var (
	ErrInvalidCatalog = errors.New("Framework 公共 API 清单无效")

	apiIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)*$`)
	modulePattern      = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	versionPattern     = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)
	signaturePattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	capabilityIDRegexp = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9-]*)*$`)
	callerIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)*$`)
)

type Kind string

const (
	KindPackage  Kind = "package"
	KindType     Kind = "type"
	KindFunction Kind = "function"
	KindMethod   Kind = "method"
	KindCLI      Kind = "cli"
	KindProtocol Kind = "protocol"
)

type Stability string

const (
	StabilityExperimental Stability = "experimental"
	StabilityCandidate    Stability = "candidate"
	StabilityStable       Stability = "stable"
	StabilityDeprecated   Stability = "deprecated"
)

type API struct {
	APIID           string    `json:"api_id"`
	Module          string    `json:"module"`
	Package         string    `json:"package"`
	Symbol          string    `json:"symbol"`
	Kind            Kind      `json:"kind"`
	Stability       Stability `json:"stability"`
	Since           string    `json:"since"`
	DeprecatedSince *string   `json:"deprecated_since"`
	Replacement     *string   `json:"replacement"`
	RemoveAfter     *string   `json:"remove_after,omitempty"`
	MigrationGuide  *string   `json:"migration_guide,omitempty"`
	SignatureSHA256 string    `json:"signature_sha256"`
	Capabilities    []string  `json:"capabilities"`
	Notes           []string  `json:"notes"`
}

type Governance struct {
	Coverage       CoveragePolicy    `json:"coverage"`
	Deprecation    DeprecationPolicy `json:"deprecation"`
	DualCallerGate DualCallerGate    `json:"dual_caller_gate"`
}

type CoveragePolicy struct {
	RequiredPackages              []string `json:"required_packages"`
	MinimumPackageCoveragePercent float64  `json:"minimum_package_coverage_percent"`
	StrictCallsite                bool     `json:"strict_callsite,omitempty"`
}

type DeprecationPolicy struct {
	MinimumWindowMinor    uint64 `json:"minimum_window_minor"`
	RequireMigrationGuide bool   `json:"require_migration_guide"`
}

type DualCallerGate struct {
	RequiredCallers []string `json:"required_callers"`
}

type Catalog struct {
	SchemaRef           string     `json:"$schema"`
	SchemaVersion       int        `json:"schema_version"`
	CatalogRevision     uint64     `json:"catalog_revision"`
	FrameworkAPIVersion string     `json:"framework_api_version"`
	Governance          Governance `json:"governance"`
	APIs                []API      `json:"apis"`

	byID map[string]API
}

func Decode(data []byte) (Catalog, error) {
	var catalog Catalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	if decoder.More() {
		return Catalog{}, ErrInvalidCatalog
	}
	if err := catalog.validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (catalog Catalog) Digest() (string, error) {
	if len(catalog.byID) == 0 {
		return "", ErrInvalidCatalog
	}
	canonical, err := json.Marshal(struct {
		SchemaVersion       int        `json:"schema_version"`
		CatalogRevision     uint64     `json:"catalog_revision"`
		FrameworkAPIVersion string     `json:"framework_api_version"`
		Governance          Governance `json:"governance"`
		APIs                []API      `json:"apis"`
	}{catalog.SchemaVersion, catalog.CatalogRevision, catalog.FrameworkAPIVersion, catalog.Governance, catalog.APIs})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCatalog, err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func (catalog Catalog) Lookup(id string) (API, bool) {
	api, ok := catalog.byID[id]
	return api, ok
}

func (catalog Catalog) IDs() []string {
	ids := make([]string, 0, len(catalog.APIs))
	for _, api := range catalog.APIs {
		ids = append(ids, api.APIID)
	}
	return ids
}

func (catalog *Catalog) validate() error {
	if catalog.SchemaVersion != SchemaVersion || catalog.CatalogRevision == 0 || !versionPattern.MatchString(catalog.FrameworkAPIVersion) || len(catalog.APIs) == 0 {
		return ErrInvalidCatalog
	}
	if err := validateGovernancePolicy(catalog.Governance); err != nil {
		return err
	}
	catalog.byID = make(map[string]API, len(catalog.APIs))
	for _, api := range catalog.APIs {
		if err := validateAPI(api); err != nil {
			return err
		}
		if _, exists := catalog.byID[api.APIID]; exists {
			return fmt.Errorf("%w: 重复 api_id %s", ErrInvalidCatalog, api.APIID)
		}
		catalog.byID[api.APIID] = api
	}
	if !sort.StringsAreSorted(catalog.IDs()) {
		return fmt.Errorf("%w: api_id 必须排序", ErrInvalidCatalog)
	}
	return nil
}

func validateAPI(api API) error {
	if !apiIDPattern.MatchString(api.APIID) || !modulePattern.MatchString(api.Module) ||
		strings.TrimSpace(api.Package) == "" || strings.TrimSpace(api.Symbol) == "" ||
		!versionPattern.MatchString(api.Since) || !signaturePattern.MatchString(api.SignatureSHA256) ||
		hasDuplicates(api.Capabilities) || hasDuplicates(api.Notes) || len(api.Capabilities) == 0 || len(api.Notes) == 0 {
		return ErrInvalidCatalog
	}
	switch api.Kind {
	case KindPackage, KindType, KindFunction, KindMethod, KindCLI, KindProtocol:
	default:
		return ErrInvalidCatalog
	}
	switch api.Stability {
	case StabilityExperimental, StabilityCandidate, StabilityStable:
		if api.DeprecatedSince != nil || api.RemoveAfter != nil || api.MigrationGuide != nil {
			return ErrInvalidCatalog
		}
	case StabilityDeprecated:
		if api.DeprecatedSince == nil || !versionPattern.MatchString(*api.DeprecatedSince) || api.Replacement == nil || strings.TrimSpace(*api.Replacement) == "" {
			return ErrInvalidCatalog
		}
	default:
		return ErrInvalidCatalog
	}
	for _, capability := range api.Capabilities {
		if !capabilityIDRegexp.MatchString(capability) {
			return ErrInvalidCatalog
		}
	}
	return nil
}

func validateGovernancePolicy(governance Governance) error {
	if len(governance.Coverage.RequiredPackages) == 0 ||
		governance.Coverage.MinimumPackageCoveragePercent < 0 ||
		governance.Coverage.MinimumPackageCoveragePercent > 100 ||
		governance.Deprecation.MinimumWindowMinor == 0 ||
		len(governance.DualCallerGate.RequiredCallers) == 0 ||
		hasDuplicates(governance.Coverage.RequiredPackages) ||
		hasDuplicates(governance.DualCallerGate.RequiredCallers) {
		return ErrInvalidCatalog
	}
	for _, pkg := range governance.Coverage.RequiredPackages {
		if !strings.HasPrefix(pkg, "framework/go/") && !strings.HasPrefix(pkg, "tools/") {
			return ErrInvalidCatalog
		}
	}
	for _, caller := range governance.DualCallerGate.RequiredCallers {
		if !callerIDPattern.MatchString(caller) {
			return ErrInvalidCatalog
		}
	}
	return nil
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
