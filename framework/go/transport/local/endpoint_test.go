package local

import (
	"path/filepath"
	"testing"
)

func TestEndpointValidate(t *testing.T) {
	t.Parallel()

	valid := Endpoint{Name: "sample-agent", RunDir: t.TempDir()}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法端点校验失败: %v", err)
	}

	tests := []struct {
		name     string
		endpoint Endpoint
	}{
		{name: "名称为空", endpoint: Endpoint{RunDir: t.TempDir()}},
		{name: "运行目录为空", endpoint: Endpoint{Name: "sample-agent"}},
		{name: "名称包含路径分隔符", endpoint: Endpoint{Name: "sample/agent", RunDir: t.TempDir()}},
		{name: "名称包含 Windows 非法字符", endpoint: Endpoint{Name: "sample:agent", RunDir: t.TempDir()}},
		{name: "名称包含控制字符", endpoint: Endpoint{Name: "sample\nagent", RunDir: t.TempDir()}},
		{name: "名称包含空格", endpoint: Endpoint{Name: "sample agent", RunDir: t.TempDir()}},
		{name: "名称包含非 ASCII 字符", endpoint: Endpoint{Name: "示例端点", RunDir: t.TempDir()}},
		{name: "名称以点开头", endpoint: Endpoint{Name: ".sample-agent", RunDir: t.TempDir()}},
		{name: "名称以点结尾", endpoint: Endpoint{Name: "sample-agent.", RunDir: t.TempDir()}},
		{name: "运行目录不是绝对路径", endpoint: Endpoint{Name: "sample-agent", RunDir: filepath.Join("relative", "run")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.endpoint.Validate(); err == nil {
				t.Fatal("非法端点未被拒绝")
			}
		})
	}
}

func TestEndpointTarget(t *testing.T) {
	t.Parallel()
	endpoint := Endpoint{Name: "sample-agent", RunDir: t.TempDir()}
	if got, want := endpoint.Target(), "passthrough:///sample-agent"; got != want {
		t.Fatalf("gRPC target = %q，期望 %q", got, want)
	}
}
