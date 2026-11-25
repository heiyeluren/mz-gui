package capability

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownFieldsAndInvalidGraphs(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: strings.Replace(validCatalogJSON(), `"catalog_revision":1`, `"catalog_revision":1,"unexpected":true`, 1)},
		{name: "重复能力", data: strings.Replace(validCatalogJSON(), `]}`, `, {"capability_id":"storage.sqlite","name":"重复","module":"storage","maturity":"implemented","api_stability":"experimental","platforms":["macos-arm64"],"depends_on":[],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/storage/sqlite"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["sqlite"],"owner":"framework-storage","verified_artifact":null,"verified_at":null,"known_limitations":[]}]}`, 1)},
		{name: "未知依赖", data: strings.Replace(validCatalogJSON(), `"depends_on":[]`, `"depends_on":["storage.missing"]`, 1)},
		{name: "依赖环", data: strings.Replace(validCatalogJSON(), `"depends_on":[]`, `"depends_on":["runtime.task"]`, 1)},
		{name: "非法成熟度", data: strings.Replace(validCatalogJSON(), `"maturity":"implemented"`, `"maturity":"done"`, 1)},
		{name: "非法验证日期", data: strings.Replace(strings.Replace(strings.Replace(validCatalogJSON(), `"maturity":"implemented"`, `"maturity":"cross-platform-verified"`, 1), `"verified_artifact":null`, `"verified_artifact":"matrix.json"`, 1), `"verified_at":null`, `"verified_at":"not-a-date"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode([]byte(test.data)); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("应拒绝非法 Catalog，实际为 %v", err)
			}
		})
	}
}

func TestResolveReturnsStableDependencyClosure(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := catalog.Resolve([]string{"runtime.scheduler", "storage.sqlite", "runtime.scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	wantRequested := []string{"runtime.scheduler", "storage.sqlite"}
	wantResolved := []string{"storage.sqlite", "runtime.task", "runtime.scheduler"}
	if !reflect.DeepEqual(selection.Requested, wantRequested) || !reflect.DeepEqual(selection.Resolved, wantResolved) {
		t.Fatalf("能力闭包不稳定: requested=%v resolved=%v", selection.Requested, selection.Resolved)
	}
	if selection.CatalogRevision != 1 || selection.FrameworkAPIVersion != "1.0.0" {
		t.Fatalf("选择结果缺少 Catalog 身份: %#v", selection)
	}
	digest, err := selection.Digest()
	if err != nil || len(digest) != 64 {
		t.Fatalf("能力选择摘要无效: digest=%q err=%v", digest, err)
	}
	reordered := selection
	reordered.Resolved = []string{"runtime.scheduler", "runtime.task", "storage.sqlite"}
	reorderedDigest, err := reordered.Digest()
	if err != nil || reorderedDigest == digest {
		t.Fatalf("能力解析顺序必须进入摘要: first=%q reordered=%q err=%v", digest, reorderedDigest, err)
	}
	duplicate := selection
	duplicate.Resolved = append(duplicate.Resolved, duplicate.Resolved[0])
	if _, err := duplicate.Digest(); !errors.Is(err, ErrInvalidSelection) {
		t.Fatalf("重复能力选择必须拒绝，实际为 %v", err)
	}
}

func TestProductionValidationRequiresVerifiedMaturityAndEvidence(t *testing.T) {
	catalog, err := Decode([]byte(validCatalogJSON()))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := catalog.Resolve([]string{"runtime.scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateProduction(selection); !errors.Is(err, ErrMaturityInsufficient) {
		t.Fatalf("implemented 能力不得通过生产门禁，实际为 %v", err)
	}

	verified := strings.ReplaceAll(validCatalogJSON(), `"maturity":"implemented"`, `"maturity":"cross-platform-verified"`)
	verified = strings.ReplaceAll(verified, `"verified_artifact":null`, `"verified_artifact":"release/matrix-1.json"`)
	verified = strings.ReplaceAll(verified, `"verified_at":null`, `"verified_at":"2026-08-15T00:00:00Z"`)
	catalog, err = Decode([]byte(verified))
	if err != nil {
		t.Fatal(err)
	}
	selection, _ = catalog.Resolve([]string{"runtime.scheduler"})
	if err := catalog.ValidateProduction(selection); err != nil {
		t.Fatalf("有完整证据的跨平台能力应通过生产门禁: %v", err)
	}
}

func TestRepositoryCatalogIsValidAndKeepsCurrentEvidenceHonest(t *testing.T) {
	data, err := os.ReadFile("../../../capabilities/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"lifecycle", "window.main", "storage.config", "storage.sqlite", "runtime.resource-pool", "runtime.resilience", "runtime.task", "runtime.scheduler", "runtime.terminal", "platform.credential", "transport.local", "security.capability", "adapter.browser"} {
		if _, ok := catalog.Lookup(id); !ok {
			t.Errorf("初始 Catalog 缺少能力 %s", id)
		}
	}
	terminalSelection, err := catalog.Resolve([]string{"runtime.terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"runtime.resource-pool", "runtime.terminal"}; !reflect.DeepEqual(terminalSelection.Resolved, want) {
		t.Fatalf("终端能力必须闭包资源池: want=%v got=%v", want, terminalSelection.Resolved)
	}
	selection, err := catalog.Resolve([]string{"runtime.scheduler", "adapter.browser"})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateProduction(selection); !errors.Is(err, ErrMaturityInsufficient) {
		t.Fatalf("Windows 真机门禁未完成前不得误标生产可用，实际为 %v", err)
	}
}

func validCatalogJSON() string {
	return `{"schema_version":1,"catalog_revision":1,"framework_api_version":"1.0.0","capabilities":[
{"capability_id":"storage.sqlite","name":"SQLite","module":"storage","maturity":"implemented","api_stability":"experimental","platforms":["macos-arm64","windows-x64"],"depends_on":[],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/storage/sqlite"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["sqlite"],"owner":"framework-storage","verified_artifact":null,"verified_at":null,"known_limitations":["缺少 Windows 真机验证"]},
{"capability_id":"runtime.task","name":"任务运行时","module":"runtime","maturity":"implemented","api_stability":"experimental","platforms":["macos-arm64","windows-x64"],"depends_on":["storage.sqlite"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/taskengine"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["task"],"owner":"framework-runtime","verified_artifact":null,"verified_at":null,"known_limitations":["缺少 Windows 真机验证"]},
{"capability_id":"runtime.scheduler","name":"调度运行时","module":"runtime","maturity":"implemented","api_stability":"experimental","platforms":["macos-arm64","windows-x64"],"depends_on":["runtime.task"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/scheduler"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["scheduler"],"owner":"framework-runtime","verified_artifact":null,"verified_at":null,"known_limitations":["缺少 Windows 真机验证"]}]}`
}
