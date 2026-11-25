package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunGenerateAndCheckModes(t *testing.T) {
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "mzgui.app.toml")
	catalogPath := filepath.Join(directory, "catalog.json")
	outputPath := filepath.Join(directory, "manifest_gen.go")
	manifest, catalog := cliFixtures()
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, catalog, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if code := run([]string{"--manifest", manifestPath, "--catalog", catalogPath, "--output", outputPath, "--package", "appmeta"}, &stdout); code != 0 {
		t.Fatalf("生成模式退出码=%d, output=%s", code, stdout.String())
	}
	before, _ := os.ReadFile(outputPath)
	if code := run([]string{"--check", "--manifest", manifestPath, "--catalog", catalogPath, "--output", outputPath, "--package", "appmeta"}, &stdout); code != 0 {
		t.Fatalf("检查模式退出码=%d, output=%s", code, stdout.String())
	}
	after, _ := os.ReadFile(outputPath)
	if !bytes.Equal(before, after) {
		t.Fatal("检查模式不得修改输出文件")
	}
}

func cliFixtures() ([]byte, []byte) {
	catalog := []byte(`{"schema_version":1,"catalog_revision":1,"framework_api_version":"1.0.0","capabilities":[{"capability_id":"lifecycle","name":"Lifecycle","module":"lifecycle","maturity":"implemented","api_stability":"experimental","platforms":["macos-arm64"],"depends_on":[],"default_enabled":false,"degradation":"fail-startup","public_contracts":["framework/go/lifecycle"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["lifecycle"],"owner":"core","verified_artifact":null,"verified_at":null,"known_limitations":[]}]}`)
	manifest := []byte(`schema_version = 1
[app]
id = "com.example.product"
display_name = "Example"
description = "Example"
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
capabilities = ["lifecycle"]
adapters = []
[security]
credential_service = "com.example.product.credentials"
[build]
macos_bundle_id = "com.example.product"
windows_app_id = "Example.Product"
channels = ["dev"]
`)
	return manifest, catalog
}
