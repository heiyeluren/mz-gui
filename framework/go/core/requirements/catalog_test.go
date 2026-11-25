package requirements

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/platform/macosx86"
	"github.com/heiyeluren/mz-gui/framework/go/platform/winremote"
)

func TestDecodeRejectsInvalidRequirementMatrices(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "未知字段", data: strings.Replace(validMatrixJSON(), `"release":"1.0.0"`, `"release":"1.0.0","unknown":true`, 1)},
		{name: "重复需求", data: strings.Replace(validMatrixJSON(), `]}`, `,{"requirement_id":"FW-CORE-001","phase":"F1","domain":"core","title":"重复","priority":"P0","implementation_status":"missing","design_refs":["5.1"],"implementation_refs":[],"contract_refs":[],"test_refs":[],"evidence_refs":[],"gaps":["未实现"],"platform_verification":{"macos-arm64":"not-verified","macos-x86_64":"not-verified","windows-x64":"not-verified"}}]}`, 1)},
		{name: "非法状态", data: strings.Replace(validMatrixJSON(), `"implementation_status":"partial"`, `"implementation_status":"done"`, 1)},
		{name: "缺少平台", data: strings.Replace(validMatrixJSON(), `,"windows-x64":"cross-compiled"`, ``, 1)},
		{name: "已移除的 windows-arm64", data: strings.Replace(validMatrixJSON(), `"windows-x64":"cross-compiled"}`, `"windows-x64":"cross-compiled","windows-arm64":"cross-compiled"}`, 1)},
		{name: "部分实现无缺口", data: strings.Replace(validMatrixJSON(), `"gaps":["缺少错误 registry"]`, `"gaps":[]`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode([]byte(test.data)); !errors.Is(err, ErrInvalidCatalog) {
				t.Fatalf("应拒绝非法需求矩阵，实际为 %v", err)
			}
		})
	}
}

func TestSummaryKeepsImplementationAndVerificationSeparate(t *testing.T) {
	catalog, err := Decode([]byte(validMatrixJSON()))
	if err != nil {
		t.Fatal(err)
	}
	summary := catalog.Summary()
	if summary.Total != 2 || summary.Implemented != 1 || summary.Partial != 1 || summary.Missing != 0 {
		t.Fatalf("实现状态汇总错误: %#v", summary)
	}
	if summary.Platforms["macos-arm64"].LocalAutomated != 2 || summary.Platforms["windows-x64"].CrossCompiled != 2 {
		t.Fatalf("平台验证汇总错误: %#v", summary.Platforms)
	}
}

func TestRepositoryMatrixCoversTheFrameworkV1DesignHonestly(t *testing.T) {
	root := projectRoot(t)
	schemaData, err := os.ReadFile(filepath.Join(root, "framework", "requirements", "v1.0.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schemaData) {
		t.Fatal("Framework v1.0 JSON Schema 必须是有效 JSON")
	}
	data, err := os.ReadFile(filepath.Join(root, "framework", "requirements", "v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SchemaRef != "./v1.0.schema.json" {
		t.Fatalf("Framework v1.0 需求矩阵必须引用同目录 schema: %q", catalog.SchemaRef)
	}
	wantIDs := []string{
		"FW-ARCH-001", "FW-ARCH-002", "FW-XPLAT-001", "FW-IPC-001", "FW-RUNTIME-001",
		"FW-SEC-001", "FW-UI-001", "FW-STATE-001", "FW-NET-001", "FW-CONFIG-001",
		"FW-DATA-001", "FW-CAP-001", "FW-CORE-001", "FW-API-001", "FW-WEBVIEW-001",
		"FW-EXT-001", "FW-OBS-001", "FW-TEST-001", "FW-REL-001",
	}
	if !reflect.DeepEqual(catalog.IDs(), wantIDs) {
		t.Fatalf("Framework v1.0 需求集合漂移: %#v", catalog.IDs())
	}
	if err := catalog.ValidateRepository(root); err != nil {
		t.Fatal(err)
	}
	summary := catalog.Summary()
	if summary.Total != 19 || summary.Implemented != 19 || summary.Partial != 0 || summary.Missing != 0 {
		t.Fatalf("当前 Framework 缺口基线不得被误报完成: %#v", summary)
	}
	windowsEvidenceOK := false
	if data, err := os.ReadFile(filepath.Join(root, "docs", "testing", "artifacts", "windows-x64", "windows-x64-evidence.json")); err == nil {
		evidence, err := winremote.Decode(data)
		windowsEvidenceOK = err == nil && evidence.AllowsLocalAutomated()
	}
	macosX8664EvidenceOK := false
	if data, err := os.ReadFile(filepath.Join(root, "docs", "testing", "artifacts", "macos-x86_64", "macos-x86_64-evidence.json")); err == nil {
		evidence, err := macosx86.Decode(data)
		macosX8664EvidenceOK = err == nil && evidence.AllowsLocalAutomated()
	}
	for _, requirement := range catalog.Requirements {
		if requirement.PlatformVerification["macos-x86_64"] == VerificationLocalAutomated && !macosX8664EvidenceOK {
			t.Errorf("没有 macos-x86_64 真机桌面构建证据，不得标记本机自动验证: %s", requirement.RequirementID)
		}
		if _, exists := requirement.PlatformVerification["windows-arm64"]; exists {
			t.Errorf("兼容设备已去掉 windows-arm64，不得再出现在矩阵中: %s", requirement.RequirementID)
		}
		if requirement.PlatformVerification["windows-x64"] == VerificationLocalAutomated && !windowsEvidenceOK {
			t.Errorf("没有 windows-x64 真机证据，不得标记本机自动验证: %s", requirement.RequirementID)
		}
	}
}

func TestWindowsX64HonestyDoesNotInventLocalAutomatedOrWindowsArm(t *testing.T) {
	root := projectRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "framework", "requirements", "v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	// 第一包真机只覆盖 identify/deviceprobe/framework-go-test/desktop-build。
	// 不得把其余需求扩成 local-automated，也不得把 windows-arm 写回矩阵。
	allowedLocalAutomated := map[string]struct{}{}
	gotLocal := make([]string, 0, 3)
	for _, requirement := range catalog.Requirements {
		if _, exists := requirement.PlatformVerification["windows-arm64"]; exists {
			t.Errorf("不得伪造 windows-arm 兼容设备: %s", requirement.RequirementID)
		}
		status := requirement.PlatformVerification["windows-x64"]
		switch status {
		case VerificationLocalAutomated:
			if _, ok := allowedLocalAutomated[requirement.RequirementID]; !ok {
				t.Errorf("不得伪造 windows-x64 local-automated: %s", requirement.RequirementID)
			}
			gotLocal = append(gotLocal, requirement.RequirementID)
		case VerificationCrossCompiled:
		default:
			t.Errorf("windows-x64 验证状态必须是历史证据或交叉编译: %s=%s", requirement.RequirementID, status)
		}
	}
	if len(gotLocal) != len(allowedLocalAutomated) {
		t.Fatalf("windows-x64 local-automated 白名单漂移: %#v", gotLocal)
	}

	schemaData, err := os.ReadFile(filepath.Join(root, "framework", "requirements", "v1.0.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(schemaData), "windows-arm64") {
		t.Fatal("需求矩阵 schema 不得把 windows-arm64 写回兼容设备")
	}
	capabilitySchema, err := os.ReadFile(filepath.Join(root, "framework", "capabilities", "catalog.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(capabilitySchema), "windows-arm64") {
		t.Fatal("能力目录 schema 不得把 windows-arm64 写回兼容设备")
	}

	evidenceData, err := os.ReadFile(filepath.Join(root, "docs", "testing", "artifacts", "windows-x64", "windows-x64-evidence.json"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("独立公共仓不包含历史 Windows 真机证据")
	}
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := winremote.Decode(evidenceData)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Platform != winremote.PlatformWindowsX64 || evidence.GOOS != "windows" || evidence.GOARCH != "amd64" {
		t.Fatalf("历史证据必须是 windows/amd64，不得冒充 windows-arm: %#v", evidence)
	}
	if !evidence.AllowsLocalAutomated() {
		t.Fatal("历史 windows-x64 第一包证据必须仍允许既有三条 local-automated")
	}

	recordPath := filepath.Join(root, "docs", "testing", "stage-w113-g611-windows-x64-honest.md")
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("G611 必须留下 windows-x64 诚实记录: %v", err)
	}
	text := string(record)
	for _, phrase := range []string{
		"不伪造 local-automated",
		"不伪造 windows-arm",
		"未连接 192.168.1.124",
		"非本板阻塞",
		"FW-XPLAT-001",
		"FW-DATA-001",
		"FW-TEST-001",
		"LAPTOP-P1QAHCT5",
		"cross-compiled",
	} {
		if !strings.Contains(text, phrase) {
			t.Errorf("诚实记录缺少关键口径 %q", phrase)
		}
	}
	if strings.Contains(text, "本轮连接 192.168.1.124") {
		t.Fatal("诚实记录不得声称本轮连了旧 Windows 机")
	}
	if strings.Contains(text, "本轮 accepted") || strings.Contains(text, "已 accepted") {
		t.Fatal("诚实记录不得把本轮标成 accepted")
	}
}

func TestValidateRepositoryRejectsEscapingAndApplicationCoupling(t *testing.T) {
	catalog, err := Decode([]byte(validMatrixJSON()))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "framework", "go", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "framework", "go", "core", "core.go"), []byte("package core\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "core_test.go"), []byte("package tests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateRepository(root); err != nil {
		t.Fatalf("合法仓库引用应通过: %v", err)
	}

	catalog.Requirements[0].ImplementationRefs = []string{"../outside.go"}
	if err := catalog.ValidateRepository(root); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("应拒绝路径逃逸，实际为 %v", err)
	}
	catalog.Requirements[0].ImplementationRefs = []string{"apps/mozi/desktop/bootstrap"}
	if err := catalog.ValidateRepository(root); !errors.Is(err, ErrApplicationCoupling) {
		t.Fatalf("应拒绝 Framework 需求引用应用实现，实际为 %v", err)
	}
}

func TestValidateRepositoryRejectsSymlinkEscapesAndApplicationCoupling(t *testing.T) {
	catalog, err := Decode([]byte(validMatrixJSON()))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "framework", "go", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "framework", "go", "core", "core.go"), []byte("package core\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "core_test.go"), []byte("package tests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "escaped.go")); err != nil {
		t.Skipf("当前平台不支持创建测试符号链接: %v", err)
	}
	catalog.Requirements[0].ImplementationRefs = []string{"escaped.go"}
	if err := catalog.ValidateRepository(root); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("应拒绝符号链接路径逃逸，实际为 %v", err)
	}

	appDir := filepath.Join(root, "apps", "mozi")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appFile := filepath.Join(appDir, "service.go")
	if err := os.WriteFile(appFile, []byte("package mozi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(appFile, filepath.Join(root, "framework-service.go")); err != nil {
		t.Skipf("当前平台不支持创建测试符号链接: %v", err)
	}
	catalog.Requirements[0].ImplementationRefs = []string{"framework-service.go"}
	if err := catalog.ValidateRepository(root); !errors.Is(err, ErrApplicationCoupling) {
		t.Fatalf("应拒绝符号链接绕过 Framework 与应用边界，实际为 %v", err)
	}
}

func validMatrixJSON() string {
	return `{"schema_version":1,"release":"1.0.0","requirements":[
{"requirement_id":"FW-CORE-001","phase":"F1","domain":"core","title":"核心类型与错误","priority":"P0","implementation_status":"partial","design_refs":["5.1"],"implementation_refs":["framework/go/core/core.go"],"contract_refs":[],"test_refs":["tests/core_test.go"],"evidence_refs":[],"gaps":["缺少错误 registry"],"platform_verification":{"macos-arm64":"local-automated","macos-x86_64":"cross-compiled","windows-x64":"cross-compiled"}},
{"requirement_id":"FW-ARCH-001","phase":"F0","domain":"architecture","title":"边界","priority":"P0","implementation_status":"implemented","design_refs":["2","7"],"implementation_refs":["framework/go/core"],"contract_refs":[],"test_refs":["tests/core_test.go"],"evidence_refs":[],"gaps":[],"platform_verification":{"macos-arm64":"local-automated","macos-x86_64":"cross-compiled","windows-x64":"cross-compiled"}}
]}`
}

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
