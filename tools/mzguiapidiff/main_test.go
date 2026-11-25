package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReportsCompatibleDiff(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	newData := strings.TrimSuffix(validToolCatalogJSON(), `]}`) + `,{"api_id":"transport.next","module":"transport","package":"framework/go/transport/next","symbol":"Next","kind":"function","stability":"experimental","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","capabilities":["transport.remote-service"],"notes":["新增"]}]}`
	if err := os.WriteFile(newPath, []byte(newData), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--old", oldPath, "--new", newPath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("兼容 diff 不应失败: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"compatible"`) {
		t.Fatalf("JSON 输出缺少 compatible: %s", stdout.String())
	}
}

func TestRunReturnsTwoForBreakingDiff(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	newData := strings.Replace(validToolCatalogJSON(), `"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`, `"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"`, 1)
	if err := os.WriteFile(newPath, []byte(newData), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--old", oldPath, "--new", newPath}, &stdout, &stderr); code != 2 {
		t.Fatalf("breaking diff 应返回 2: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunValidatesDualCallerUsage(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	moziUsagePath := filepath.Join(dir, "mozi-usage.json")
	showcaseUsagePath := filepath.Join(dir, "showcase-usage.json")
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moziUsagePath, []byte(`{"caller_id":"mozi-desktop","api_ids":["storage.test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(showcaseUsagePath, []byte(`{"caller_id":"framework-showcase","api_ids":["missing.api"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--old", oldPath, "--new", newPath, "--caller-usage", moziUsagePath, "--caller-usage", showcaseUsagePath}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("调用方引用未登记 API 应返回 3: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	if err := os.WriteFile(showcaseUsagePath, []byte(`{"caller_id":"framework-showcase","api_ids":["storage.test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--old", oldPath, "--new", newPath, "--caller-usage", moziUsagePath, "--caller-usage", showcaseUsagePath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("双调用方均兼容时应通过: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunProvesCallsiteCoverageAndCanBeStrict(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	usagePath := filepath.Join(dir, "usage.json")
	sourceDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	showcasePath := filepath.Join(dir, "showcase.json")
	if err := os.WriteFile(usagePath, []byte(`{"caller_id":"mozi-desktop","api_ids":["storage.test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(showcasePath, []byte(`{"caller_id":"framework-showcase","api_ids":["storage.test"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "use.go"), []byte("package src\nfunc Use() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{
		"--old", oldPath, "--new", newPath,
		"--caller-usage", usagePath,
		"--caller-usage", showcasePath,
		"--callsite-caller", "mozi-desktop",
		"--callsite-root", sourceDir,
	}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Callsite mozi-desktop") {
		t.Fatalf("调用点证明应输出报告: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--old", oldPath, "--new", newPath,
		"--caller-usage", usagePath,
		"--caller-usage", showcasePath,
		"--callsite-caller", "mozi-desktop",
		"--callsite-root", sourceDir,
		"--strict-callsite",
	}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("严格调用点证明缺少命中时应返回 4: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	policyCatalog := strings.Replace(validToolCatalogJSON(), `"minimum_package_coverage_percent":100`, `"minimum_package_coverage_percent":100,"strict_callsite":true`, 1)
	if err := os.WriteFile(newPath, []byte(policyCatalog), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"--old", oldPath, "--new", newPath,
		"--caller-usage", usagePath,
		"--caller-usage", showcasePath,
		"--callsite-caller", "mozi-desktop",
		"--callsite-root", sourceDir,
	}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("catalog 启用 strict_callsite 时缺少命中应返回 4: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunProvesWailsBindingCoverage(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	bindDir := filepath.Join(dir, "boot")
	svcDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(bindDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(svcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bindDir, "app.go"), []byte("package boot\nimport \"github.com/wailsapp/wails/v3/pkg/application\"\nfunc Start(settingsService *SettingsService) { _ = application.NewService(settingsService) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.go"), []byte("package services\ntype SettingsService struct{}\nfunc (s *SettingsService) Load() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--old", oldPath, "--new", newPath, "--wails-bind-root", bindDir, "--wails-service-root", svcDir}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Wails bindings:") {
		t.Fatalf("Wails 绑定证明应输出报告: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--old", oldPath, "--new", newPath, "--wails-bind-root", bindDir}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("只提供 bind-root 应失败: code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunProvesProduceCoverageAndInventoryDrift(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	svcDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(svcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(validToolCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "service.go"), []byte("package services\ntype SettingsService struct{}\nfunc (s *SettingsService) Load() error { return nil }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventoryPath := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(inventoryPath, []byte(`{"caller_id":"mozi-desktop","gate":{"min_covered_percent":0,"strict":false},"proof":{"methods":["SettingsService.Load"],"covered":[],"missing":["SettingsService.Load"],"covered_percent":0}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run([]string{"--old", oldPath, "--new", newPath, "--produce-root", svcDir, "--produce-inventory", inventoryPath}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "Produce coverage:") {
		t.Fatalf("Produce 覆盖证明应通过: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	if err := os.WriteFile(inventoryPath, []byte(`{"caller_id":"mozi-desktop","gate":{"min_covered_percent":0,"strict":false},"proof":{"methods":[],"covered":["SettingsService.Load"],"missing":[],"covered_percent":100}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--old", oldPath, "--new", newPath, "--produce-root", svcDir, "--produce-inventory", inventoryPath}, &stdout, &stderr)
	if code != 6 {
		t.Fatalf("inventory 漂移应返回 6: code=%d stderr=%s", code, stderr.String())
	}

	if err := os.WriteFile(inventoryPath, []byte(`{"caller_id":"mozi-desktop","gate":{"min_covered_percent":50,"strict":false},"proof":{"methods":["SettingsService.Load"],"covered":[],"missing":["SettingsService.Load"],"covered_percent":0}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--old", oldPath, "--new", newPath, "--produce-root", svcDir, "--produce-inventory", inventoryPath}, &stdout, &stderr)
	if code != 6 {
		t.Fatalf("覆盖率低于门禁应返回 6: code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunRejectsMissingInputs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("缺少输入应失败: code=%d", code)
	}
}

func validToolCatalogJSON() string {
	return `{"$schema":"./public-v1.schema.json","schema_version":1,"catalog_revision":2,"framework_api_version":"1.0.0","governance":{"coverage":{"required_packages":["framework/go/storage/test"],"minimum_package_coverage_percent":100},"deprecation":{"minimum_window_minor":2,"require_migration_guide":true},"dual_caller_gate":{"required_callers":["mozi-desktop","framework-showcase"]}},"apis":[{"api_id":"storage.test","module":"storage","package":"framework/go/storage/test","symbol":"Save","kind":"function","stability":"candidate","since":"1.0.0","deprecated_since":null,"replacement":null,"signature_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","capabilities":["storage.sqlite"],"notes":["测试 API"]}]}`
}
