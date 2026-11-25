package apicatalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeRejectsInvalidCatalogs(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: strings.Replace(validCatalogJSON(), `"schema_version":1`, `"schema_version":1,"unknown":true`, 1)},
		{name: "重复 API", data: strings.Replace(validCatalogJSON(), `]}`, `,{"api_id":"core.test","module":"core","package":"framework/go/core/test","symbol":"Again","kind":"function","stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","capabilities":["core.api-catalog"],"notes":["重复"]}]}`, 1)},
		{name: "非法稳定性", data: strings.Replace(validCatalogJSON(), `"stability":"experimental"`, `"stability":"preview"`, 1)},
		{name: "非法签名", data: strings.Replace(validCatalogJSON(), `"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"abc"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode([]byte(test.data)); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("应拒绝非法 catalog，实际 %v", err)
			}
		})
	}
}

func TestDecodeRepositoryCatalogAndDigest(t *testing.T) {
	root := projectRoot(t)
	schemaData, err := os.ReadFile(filepath.Join(root, "framework", "api", "public-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaData) {
		t.Fatal("公共 API schema 必须是有效 JSON")
	}
	data, err := os.ReadFile(filepath.Join(root, "framework", "api", "public-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaRef != "./public-v1.schema.json" || len(catalog.APIs) < 5 {
		t.Fatalf("公共 API catalog 异常: %#v", catalog)
	}
	if digest, err := catalog.Digest(); err != nil || len(digest) != 64 {
		t.Fatalf("digest 异常: digest=%q err=%v", digest, err)
	}
}

func TestCompareDetectsBreakingChangesAndWarnings(t *testing.T) {
	oldCatalog := mustDecode(t, validCatalogJSON())
	removed := strings.Replace(validCatalogJSON(), `{"api_id":"core.test","module":"core","package":"framework/go/core/test","symbol":"Do","kind":"function","stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","capabilities":["core.api-catalog"],"notes":["测试 API"]},`, ``, 1)
	removed = strings.Replace(removed, `"signature_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`, `"signature_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"`, 1)
	newCatalog := mustDecode(t, removed)
	diff := Compare(oldCatalog, newCatalog)
	if len(diff.Breaking) != 1 || diff.Breaking[0].Kind != ChangeSignatureChanged {
		t.Fatalf("breaking diff 异常: %#v", diff)
	}
	if len(diff.Compatible) != 1 || diff.Compatible[0].Kind != ChangeRemoved {
		t.Fatalf("experimental 删除应兼容: %#v", diff)
	}

	deprecatedOld := strings.Replace(validCatalogJSON(), `"stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null`, `"stability":"deprecated","since":"1.0.0","deprecated_since":"1.0.0","replacement":"core.next"`, 1)
	deprecatedDiff := Compare(mustDecode(t, deprecatedOld), newCatalog)
	if len(deprecatedDiff.Warnings) != 1 || deprecatedDiff.Warnings[0].Kind != ChangeDeprecatedRemoved {
		t.Fatalf("deprecated 删除应输出 warning: %#v", deprecatedDiff)
	}
}

func TestCoverageReportDetectsMissingRequiredPackages(t *testing.T) {
	catalog := mustDecode(t, validCatalogJSON())

	report := catalog.CoverageReport()
	if report.RequiredPackages != 3 || report.CoveredPackages != 2 || report.PackageCoveragePercent != 66.67 {
		t.Fatalf("覆盖率报告异常: %#v", report)
	}
	if len(report.MissingPackages) != 1 || report.MissingPackages[0] != "framework/go/transport/missing" {
		t.Fatalf("缺失包列表异常: %#v", report.MissingPackages)
	}
	if err := ValidateGovernance(catalog); err == nil {
		t.Fatal("覆盖率低于策略要求时应失败")
	}
}

func TestValidateGovernanceRequiresDeprecationWindowAndMigrationGuide(t *testing.T) {
	data := strings.Replace(completeCoverageCatalogJSON(), `"stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null`, `"stability":"deprecated","since":"1.0.0","deprecated_since":"1.0.0","replacement":"storage.test"`, 1)
	catalog := mustDecode(t, data)

	if err := ValidateGovernance(catalog); err == nil {
		t.Fatal("deprecated API 缺少移除窗口和迁移说明时应失败")
	}

	data = strings.Replace(data, `"replacement":"storage.test"`, `"replacement":"storage.test","remove_after":"1.3.0","migration_guide":"docs/migrations/core-test-to-storage-test.md"`, 1)
	catalog = mustDecode(t, data)
	if err := ValidateGovernance(catalog, CallerUsage{CallerID: "mozi-desktop", APIIDs: []string{"core.test"}}, CallerUsage{CallerID: "framework-showcase", APIIDs: []string{"storage.test"}}); err != nil {
		t.Fatalf("满足弃用窗口、迁移说明和双调用方时应通过: %v", err)
	}
}

func TestValidateGovernanceRequiresDualCallers(t *testing.T) {
	catalog := mustDecode(t, completeCoverageCatalogJSON())
	err := ValidateGovernance(catalog, CallerUsage{CallerID: "mozi-desktop", APIIDs: []string{"core.test"}})
	if err == nil {
		t.Fatal("缺少 showcase 调用方时应失败")
	}

	err = ValidateGovernance(catalog, CallerUsage{CallerID: "mozi-desktop", APIIDs: []string{"missing.api"}}, CallerUsage{CallerID: "framework-showcase", APIIDs: []string{"storage.test"}})
	if err == nil {
		t.Fatal("调用方引用未登记 API 时应失败")
	}
}

func validCatalogJSON() string {
	return `{"$schema":"./public-v1.schema.json","schema_version":1,"catalog_revision":2,"framework_api_version":"1.0.0","governance":{"coverage":{"required_packages":["framework/go/core/test","framework/go/storage/test","framework/go/transport/missing"],"minimum_package_coverage_percent":100},"deprecation":{"minimum_window_minor":2,"require_migration_guide":true},"dual_caller_gate":{"required_callers":["mozi-desktop","framework-showcase"]}},"apis":[{"api_id":"core.test","module":"core","package":"framework/go/core/test","symbol":"Do","kind":"function","stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","capabilities":["core.api-catalog"],"notes":["测试 API"]},{"api_id":"storage.test","module":"storage","package":"framework/go/storage/test","symbol":"Save","kind":"function","stability":"candidate","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capabilities":["storage.sqlite"],"notes":["测试 API"]}]}`
}

func completeCoverageCatalogJSON() string {
	return strings.Replace(validCatalogJSON(), `,"framework/go/transport/missing"`, ``, 1)
}

func mustDecode(t *testing.T, data string) Catalog {
	t.Helper()
	catalog, err := Decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("无法定位仓库根目录")
		}
		dir = parent
	}
}
