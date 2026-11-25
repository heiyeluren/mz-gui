package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunAcceptsValidProposalAndRejectsInvalid(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "framework/capabilities"), 0o755); err != nil {
		t.Fatal(err)
	}
	catalogPath := filepath.Join(root, "framework/capabilities/catalog.json")
	if err := os.WriteFile(catalogPath, []byte(testCatalogJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(root, "proposal.valid.json")
	if err := os.WriteFile(validPath, []byte(testProposalJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--root", root, "--proposal", validPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("合法 proposal 应通过: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	invalidPath := filepath.Join(root, "proposal.invalid.json")
	if err := os.WriteFile(invalidPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--root", root, "--proposal", invalidPath}, &stdout, &stderr); code != 2 {
		t.Fatalf("非法 proposal 应返回 2: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func testCatalogJSON() string {
	return `{"schema_version":1,"catalog_revision":1,"framework_api_version":"1.0.0","capabilities":[{"capability_id":"runtime.task","name":"Task","module":"runtime","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":[],"default_enabled":false,"degradation":"disable-entry","public_contracts":["framework/go/runtime/taskengine"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["task"],"owner":"framework-runtime","verified_artifact":null,"verified_at":null,"known_limitations":["缺少真机验证"]}]}`
}

func testProposalJSON() string {
	return `{"schema_version":1,"proposal_id":"20260815-observability-trace","change_type":"add","definition":{"capability_id":"observability.trace","name":"Trace Context","module":"observability","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":["runtime.task"],"default_enabled":false,"degradation":"new-root-trace","public_contracts":["framework/go/observability/tracecontext"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["framework-tracecontext"],"owner":"framework-observability","verified_artifact":null,"verified_at":null,"known_limitations":["middleware 尚未接入"]},"rationale":"为跨进程链路提供统一 trace context。","adr_refs":["docs/adr/0060-framework-observability-trace-metrics.md"],"evidence_refs":["docs/testing/stage-72-framework-observability-trace-metrics-validation.md"],"migration_notes":"新增能力默认关闭，由应用 manifest 显式声明。"}`
}
