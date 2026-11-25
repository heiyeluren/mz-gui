package apicatalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

var ErrInvalidGovernance = errors.New("Framework 公共 API 治理门禁失败")

type CoverageReport struct {
	RequiredPackages       int      `json:"required_packages"`
	CoveredPackages        int      `json:"covered_packages"`
	MissingPackages        []string `json:"missing_packages"`
	PackageCoveragePercent float64  `json:"package_coverage_percent"`
}

type CallerUsage struct {
	CallerID string   `json:"caller_id"`
	APIIDs   []string `json:"api_ids"`
}

func DecodeCallerUsage(data []byte) (CallerUsage, error) {
	var usage CallerUsage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&usage); err != nil {
		return CallerUsage{}, fmt.Errorf("%w: %v", ErrInvalidGovernance, err)
	}
	if decoder.More() || !callerIDPattern.MatchString(usage.CallerID) || len(usage.APIIDs) == 0 || hasDuplicates(usage.APIIDs) {
		return CallerUsage{}, ErrInvalidGovernance
	}
	for _, apiID := range usage.APIIDs {
		if !apiIDPattern.MatchString(apiID) {
			return CallerUsage{}, ErrInvalidGovernance
		}
	}
	return usage, nil
}

func (catalog Catalog) CoverageReport() CoverageReport {
	covered := make(map[string]struct{}, len(catalog.APIs))
	for _, api := range catalog.APIs {
		covered[api.Package] = struct{}{}
	}
	missing := make([]string, 0)
	for _, pkg := range catalog.Governance.Coverage.RequiredPackages {
		if _, ok := covered[pkg]; !ok {
			missing = append(missing, pkg)
		}
	}
	required := len(catalog.Governance.Coverage.RequiredPackages)
	coveredCount := required - len(missing)
	percent := 100.0
	if required > 0 {
		percent = math.Round((float64(coveredCount)/float64(required))*10000) / 100
	}
	return CoverageReport{
		RequiredPackages:       required,
		CoveredPackages:        coveredCount,
		MissingPackages:        missing,
		PackageCoveragePercent: percent,
	}
}

func ValidateGovernance(catalog Catalog, usages ...CallerUsage) error {
	report := catalog.CoverageReport()
	if report.PackageCoveragePercent+0.0001 < catalog.Governance.Coverage.MinimumPackageCoveragePercent {
		return fmt.Errorf("%w: API 包登记覆盖率 %.2f 低于 %.2f，缺失 %s", ErrInvalidGovernance, report.PackageCoveragePercent, catalog.Governance.Coverage.MinimumPackageCoveragePercent, strings.Join(report.MissingPackages, ","))
	}
	if err := validateDeprecations(catalog); err != nil {
		return err
	}
	if err := validateCallerUsages(catalog, usages); err != nil {
		return err
	}
	return nil
}

func validateDeprecations(catalog Catalog) error {
	for _, api := range catalog.APIs {
		if api.Stability != StabilityDeprecated {
			continue
		}
		if api.Replacement == nil {
			return fmt.Errorf("%w: %s 缺少 replacement", ErrInvalidGovernance, api.APIID)
		}
		if _, ok := catalog.Lookup(*api.Replacement); !ok {
			return fmt.Errorf("%w: %s replacement 未登记", ErrInvalidGovernance, api.APIID)
		}
		if api.RemoveAfter == nil || !versionPattern.MatchString(*api.RemoveAfter) {
			return fmt.Errorf("%w: %s 缺少 remove_after", ErrInvalidGovernance, api.APIID)
		}
		if minorDistance(*api.DeprecatedSince, *api.RemoveAfter) < catalog.Governance.Deprecation.MinimumWindowMinor {
			return fmt.Errorf("%w: %s 弃用窗口不足", ErrInvalidGovernance, api.APIID)
		}
		if catalog.Governance.Deprecation.RequireMigrationGuide && (api.MigrationGuide == nil || strings.TrimSpace(*api.MigrationGuide) == "") {
			return fmt.Errorf("%w: %s 缺少 migration_guide", ErrInvalidGovernance, api.APIID)
		}
	}
	return nil
}

func validateCallerUsages(catalog Catalog, usages []CallerUsage) error {
	byCaller := make(map[string]CallerUsage, len(usages))
	for _, usage := range usages {
		if _, exists := byCaller[usage.CallerID]; exists {
			return fmt.Errorf("%w: 重复调用方 %s", ErrInvalidGovernance, usage.CallerID)
		}
		for _, apiID := range usage.APIIDs {
			if _, ok := catalog.Lookup(apiID); !ok {
				return fmt.Errorf("%w: 调用方 %s 引用未登记 API %s", ErrInvalidGovernance, usage.CallerID, apiID)
			}
		}
		byCaller[usage.CallerID] = usage
	}
	for _, caller := range catalog.Governance.DualCallerGate.RequiredCallers {
		if _, ok := byCaller[caller]; !ok {
			return fmt.Errorf("%w: 缺少调用方 %s", ErrInvalidGovernance, caller)
		}
	}
	return nil
}

func minorDistance(from, to string) uint64 {
	fromParts := strings.Split(from, ".")
	toParts := strings.Split(to, ".")
	if len(fromParts) != 3 || len(toParts) != 3 || fromParts[0] != toParts[0] {
		return 0
	}
	var fromMinor uint64
	var toMinor uint64
	fmt.Sscanf(fromParts[1], "%d", &fromMinor)
	fmt.Sscanf(toParts[1], "%d", &toMinor)
	if toMinor < fromMinor {
		return 0
	}
	return toMinor - fromMinor
}
