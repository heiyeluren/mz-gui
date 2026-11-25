package winremote

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeAcceptsPassingWindowsX64Evidence(t *testing.T) {
	evidence, err := Decode([]byte(validEvidenceJSON()))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Platform != PlatformWindowsX64 || evidence.GOOS != "windows" || evidence.GOARCH != "amd64" {
		t.Fatalf("平台字段错误: %#v", evidence)
	}
	if !evidence.AllowsLocalAutomated() {
		t.Fatal("完整通过的 windows-x64 证据应允许标记本机自动验证")
	}
}

func TestDecodeRejectsInvalidEvidence(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
	}{
		{name: "未知字段", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"schema_version":1`, `"schema_version":1,"unexpected":true`, 1)},
		{name: "错误平台", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"platform":"windows-x64"`, `"platform":"macos-arm64"`, 1)},
		{name: "错误架构", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"goarch":"amd64"`, `"goarch":"arm64"`, 1)},
		{name: "缺少提交", wantErr: true, data: strings.Replace(validEvidenceJSON(), `"commit":"abc123def456"`, `"commit":""`, 1)},
		{name: "缺少步骤", wantErr: true, data: strings.Replace(validEvidenceJSON(), `,{"name":"desktop-build","status":"pass","detail":"mz-gui-desktop.exe"}`, ``, 1)},
		{name: "失败状态", data: strings.Replace(validEvidenceJSON(), `"status":"pass"`, `"status":"fail"`, 1)},
		{name: "步骤失败", data: strings.Replace(validEvidenceJSON(), `"name":"deviceprobe","status":"pass"`, `"name":"deviceprobe","status":"fail"`, 1)},
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

func validEvidenceJSON() string {
	return `{"schema_version":1,"platform":"windows-x64","goos":"windows","goarch":"amd64","hostname":"WIN11-DEV","commit":"abc123def456","started_at":"2026-08-16T07:48:00Z","finished_at":"2026-08-16T07:55:00Z","status":"pass","steps":[{"name":"identify","status":"pass","detail":"windows/amd64"},{"name":"deviceprobe","status":"pass","detail":"ok"},{"name":"framework-go-test","status":"pass","detail":"ok"},{"name":"desktop-build","status":"pass","detail":"mz-gui-desktop.exe"}]}`
}
