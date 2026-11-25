package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/capability"
)

func TestRunOutputsCapabilityEvidenceReport(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "framework/capabilities/catalog.json", testCatalogJSON())
	writeFixture(t, root, "framework/requirements/v1.0.json", testRequirementsJSON())

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("合法输入应通过: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var report capability.EvidenceReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Total != 1 || report.Linked != 1 || report.Unlinked != 0 {
		t.Fatalf("报告统计错误: %#v", report)
	}
	if got := report.Capabilities[0].Requirements[0].RequirementID; got != "FW-CAP-001" {
		t.Fatalf("需求关联错误: %s", got)
	}
}

func TestRunRejectsInvalidRequirements(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "framework/capabilities/catalog.json", testCatalogJSON())
	writeFixture(t, root, "framework/requirements/v1.0.json", `{"schema_version":1}`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--root", root}, &stdout, &stderr); code != 1 {
		t.Fatalf("非法 requirements 应返回 1: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func writeFixture(t *testing.T, root, path, data string) {
	t.Helper()
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testCatalogJSON() string {
	return `{"schema_version":1,"catalog_revision":1,"framework_api_version":"1.0.0","capabilities":[{"capability_id":"capability.proposal-gate","name":"Capability Proposal Gate","module":"capability","maturity":"implemented","api_stability":"candidate","platforms":["macos-arm64","windows-x64"],"depends_on":[],"default_enabled":false,"degradation":"fail-release-gate","public_contracts":["framework/go/core/capability/proposal.go","tools/mzguicapabilityproposal"],"config_schema_refs":[],"required_permissions":[],"audit_events":[],"metrics":[],"test_suites":["framework-capability-proposal"],"owner":"framework-core","verified_artifact":null,"verified_at":null,"known_limitations":["缺少证据聚合"]}]}`
}

func testRequirementsJSON() string {
	return `{"schema_version":1,"release":"1.0.0","requirements":[{"requirement_id":"FW-CAP-001","phase":"F0","domain":"capability","title":"Capability Catalog 与应用能力闭包","priority":"P0","implementation_status":"partial","design_refs":["1.6"],"implementation_refs":["framework/go/core/capability","tools/mzguicapabilityproposal"],"contract_refs":["framework/go/core/capability/proposal.go"],"test_refs":["framework/go/core/capability/proposal_test.go"],"evidence_refs":["docs/testing/stage-73-framework-capability-proposal-validation.md"],"gaps":["缺少双调用方验证"],"platform_verification":{"macos-arm64":"local-automated","macos-x86_64":"cross-compiled","windows-x64":"cross-compiled"}}]}`
}
