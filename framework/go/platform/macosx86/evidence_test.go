package macosx86

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAcceptsCompleteMacosX8664Evidence(t *testing.T) {
	evidence, err := Decode([]byte(validEvidenceJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Platform != PlatformMacosX8664 || evidence.GOOS != "darwin" || evidence.GOARCH != "amd64" {
		t.Fatalf("平台字段错误: %#v", evidence)
	}
	if !evidence.AllowsLocalAutomated() {
		t.Fatal("完整通过的 macos-x86_64 桌面构建证据应允许标记本机自动验证")
	}
}

func TestDecodeRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "未知字段", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"schema_version":1`, `"schema_version":1,"unexpected":true`, 1)},
		{name: "错误平台", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"platform":"macos-x86_64"`, `"platform":"windows-arm64"`, 1)},
		{name: "错误架构", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"goarch":"amd64"`, `"goarch":"arm64"`, 1)},
		{name: "缺少主机名", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"hostname":"bogon"`, `"hostname":""`, 1)},
		{name: "缺少桌面构建步骤", wantErr: true, data: strings.Replace(validEvidenceJSON(), `,{"name":"desktop-build-cgo1","status":"pass","detail":"mz-gui-desktop"}`, ``, 1)},
		{name: "失败状态", data: strings.Replace(validEvidenceJSON(), `"status":"pass"`, `"status":"fail"`, 1)},
		{name: "桌面构建失败", data: strings.Replace(validEvidenceJSON(), `"name":"desktop-build-cgo1","status":"pass"`, `"name":"desktop-build-cgo1","status":"fail"`, 1)},
		{name: "桌面二进制缺失", data: strings.Replace(validEvidenceJSON(), `"name":"desktop-file-cgo1","status":"pass"`, `"name":"desktop-file-cgo1","status":"fail"`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := Decode([]byte(test.data))
			if test.wantErr {
				if !errors.Is(err, ErrInvalidEvidence) {
					t.Fatalf("应拒绝非法证据，实际 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if evidence.AllowsLocalAutomated() {
				t.Fatal("失败证据不得允许标记本机自动验证")
			}
		})
	}
}

func TestRepositoryEvidenceDoesNotAllowLocalAutomated(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "testing", "artifacts", "macos-x86_64", "macos-x86_64-evidence.json"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("独立公共仓不包含历史 macOS Intel 真机证据")
	}
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := Decode(data)
	if err != nil {
		t.Fatalf("仓库内 Intel 真机证据应可解码: %v", err)
	}
	if evidence.Platform != PlatformMacosX8664 || evidence.GOOS != "darwin" || evidence.GOARCH != "amd64" {
		t.Fatalf("仓库证据平台错误: %#v", evidence)
	}
	if evidence.AllowsLocalAutomated() {
		t.Fatal("stage-129 证据桌面构建失败，不得允许标记 local-automated")
	}
	if step, ok := evidence.Step("desktop-build-cgo1"); !ok || step.Status != StatusFail {
		t.Fatalf("仓库证据必须保留 desktop-build-cgo1 失败: %#v", step)
	}
}

func TestStatusPassWithFailedRequiredEmptyStillBlocksLocalAutomated(t *testing.T) {
	data := strings.Replace(validEvidenceJSON(), `"name":"desktop-build-cgo1","status":"pass"`, `"name":"desktop-build-cgo1","status":"fail"`, 1)
	evidence, err := Decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.AllowsLocalAutomated() {
		t.Fatal("顶层 status=pass 不能掩盖 desktop-build-cgo1 失败")
	}
}

func validEvidenceJSON() string {
	return `{"schema_version":1,"platform":"macos-x86_64","goos":"darwin","goarch":"amd64","hostname":"bogon","started_at":"2026-08-22T07:28:52Z","finished_at":"2026-08-22T07:30:32Z","status":"pass","steps":[{"name":"deviceprobe","status":"pass","detail":"ok"},{"name":"desktop-build-cgo1","status":"pass","detail":"mz-gui-desktop"},{"name":"desktop-file-cgo1","status":"pass","detail":"Mach-O 64-bit executable x86_64"}]}`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
