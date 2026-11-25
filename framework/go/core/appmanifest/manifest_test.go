package appmanifest

import (
	"errors"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/capability"
)

func TestParseRejectsUnknownFieldsAndUnsafeIdentity(t *testing.T) {
	catalog := testCatalog(t)
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: validManifest() + "\n[unknown]\nvalue = true\n"},
		{name: "配置路径穿越", data: strings.Replace(validManifest(), `config_basename = "example.toml"`, `config_basename = "../example.toml"`, 1)},
		{name: "Windows 保留进程名", data: strings.Replace(validManifest(), `desktop = "example-desktop"`, `desktop = "con"`, 1)},
		{name: "进程名重复", data: strings.Replace(validManifest(), `agentd = "example-agentd"`, `agentd = "example-desktop"`, 1)},
		{name: "非法协议", data: strings.Replace(validManifest(), `protocol_scheme = "example"`, `protocol_scheme = "Example App"`, 1)},
		{name: "未知能力", data: strings.Replace(validManifest(), `"runtime.scheduler"`, `"runtime.unknown"`, 1)},
		{name: "未知适配器", data: strings.Replace(validManifest(), `adapters = []`, `adapters = ["missing"]`, 1)},
		{name: "缺少显式依赖", data: strings.Replace(validManifest(), `"storage.sqlite", `, ``, 1)},
		{name: "Secret 字段", data: validManifest() + "\n[security.secrets]\ntoken = \"private\"\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.data), catalog); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("应拒绝非法 Manifest，实际为 %v", err)
			}
		})
	}
}

func TestParseReturnsValidatedExplicitSelection(t *testing.T) {
	validated, err := Parse([]byte(validManifest()), testCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if validated.Manifest.App.ID != "com.example.product" || validated.Manifest.Processes.Agentd != "example-agentd" {
		t.Fatalf("Manifest 身份解析不完整: %#v", validated.Manifest)
	}
	want := []string{"storage.sqlite", "runtime.task", "runtime.scheduler"}
	if strings.Join(validated.Selection.Resolved, ",") != strings.Join(want, ",") {
		t.Fatalf("能力选择错误: %v", validated.Selection.Resolved)
	}
}

func TestProductionValidationDelegatesToCapabilityMaturity(t *testing.T) {
	catalog := testCatalog(t)
	validated, err := Parse([]byte(validManifest()), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := validated.ValidateProduction(catalog); !errors.Is(err, capability.ErrMaturityInsufficient) {
		t.Fatalf("未完成跨平台验证的能力不得通过生产门禁，实际为 %v", err)
	}
}

func testCatalog(t *testing.T) capability.Catalog {
	t.Helper()
	data := `{"schema_version":1,"catalog_revision":3,"framework_api_version":"1.0.0","capabilities":[
{"capability_id":"storage.sqlite","name":"SQLite","module":"storage","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":[],"default_enabled":false,"degradation":"fail-startup","public_contracts":["framework/go/storage/sqlite"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["sqlite"],"owner":"storage","verified_artifact":null,"verified_at":null,"known_limitations":[]},
{"capability_id":"runtime.task","name":"Task","module":"runtime","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":["storage.sqlite"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/taskengine"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["task"],"owner":"runtime","verified_artifact":null,"verified_at":null,"known_limitations":[]},
{"capability_id":"runtime.scheduler","name":"Scheduler","module":"runtime","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":["runtime.task"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/scheduler"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["scheduler"],"owner":"runtime","verified_artifact":null,"verified_at":null,"known_limitations":[]}]}`
	catalog, err := capability.Decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func validManifest() string {
	return `schema_version = 1

[app]
id = "com.example.product"
display_name = "Example"
description = "Example desktop application"
protocol_scheme = "example"
data_namespace = "example"
single_instance_id = "com.example.product.desktop"

[processes]
desktop = "example-desktop"
agentd = "example-agentd"
browser_host = "example-browser-host"
helper = "example-helper"

[storage]
config_basename = "example.toml"
database_basename = "example.db"
artifact_namespace = "example"

[framework]
capability_catalog_version = 1
capabilities = ["storage.sqlite", "runtime.task", "runtime.scheduler"]
adapters = []

[security]
credential_service = "com.example.product.credentials"

[build]
macos_bundle_id = "com.example.product"
windows_app_id = "Example.Product"
channels = ["dev", "stable"]
`
}
