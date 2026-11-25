package appgenerator

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderIsDeterministicAndProducesValidGo(t *testing.T) {
	manifest, catalog := generatorFixtures()
	first, err := Render(manifest, catalog, Options{PackageName: "appmeta"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(manifest, catalog, Options{PackageName: "appmeta"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("相同输入必须生成完全一致的字节")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "manifest_gen.go", first, parser.AllErrors); err != nil {
		t.Fatalf("生成结果不是合法 Go 源码: %v", err)
	}
	content := string(first)
	for _, expected := range []string{
		`AppID`, `"com.example.product"`, `DesktopProcess`, `"example-desktop"`,
		`CatalogRevision`, `uint64`, `"storage.sqlite"`, `"runtime.task"`, `"runtime.scheduler"`,
		`ReleaseEligible`, `false`, `CatalogSHA256`, `ManifestSHA256`, `SelectionSHA256`,
		`func RequestedCapabilities() []string`, `func ResolvedCapabilities() []string`,
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("生成结果缺少 %q", expected)
		}
	}
	if strings.Contains(content, "var RequestedCapabilities") || strings.Contains(content, "var ResolvedCapabilities") {
		t.Fatal("生成能力选择不得暴露可修改的全局数组")
	}
}

func TestGenerateFileDoesNotOverwriteOnInvalidInput(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "mzgui.app.toml")
	catalogPath := filepath.Join(directory, "catalog.json")
	outputPath := filepath.Join(directory, "manifest_gen.go")
	manifest, catalog := generatorFixtures()
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("old-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("schema_version = 99"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GenerateFile(manifestPath, catalogPath, outputPath, Options{PackageName: "appmeta"}); err == nil {
		t.Fatal("非法 Manifest 必须导致生成失败")
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old-content" {
		t.Fatalf("失败生成不得覆盖旧文件: %q", content)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := GenerateFile(manifestPath, catalogPath, outputPath, Options{PackageName: "appmeta"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(manifestPath, catalogPath, outputPath, Options{PackageName: "appmeta"}); err != nil {
		t.Fatalf("刚生成的文件应通过检查: %v", err)
	}
}

func generatorFixtures() ([]byte, []byte) {
	catalog := []byte(`{"schema_version":1,"catalog_revision":3,"framework_api_version":"1.0.0","capabilities":[
{"capability_id":"storage.sqlite","name":"SQLite","module":"storage","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":[],"default_enabled":false,"degradation":"fail-startup","public_contracts":["framework/go/storage/sqlite"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["sqlite"],"owner":"storage","verified_artifact":null,"verified_at":null,"known_limitations":[]},
{"capability_id":"runtime.task","name":"Task","module":"runtime","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":["storage.sqlite"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/taskengine"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["task"],"owner":"runtime","verified_artifact":null,"verified_at":null,"known_limitations":[]},
{"capability_id":"runtime.scheduler","name":"Scheduler","module":"runtime","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":["runtime.task"],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/scheduler"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["scheduler"],"owner":"runtime","verified_artifact":null,"verified_at":null,"known_limitations":[]}]}`)
	manifest := []byte(`schema_version = 1
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
`)
	return manifest, catalog
}
