package local

import (
	"strings"
	"testing"
)

func TestPipeNameUsesSidSHA256Prefix(t *testing.T) {
	t.Parallel()
	got := PipeName("mz-gui-agentd", "S-1-5-21-1000")
	if !strings.HasPrefix(got, `\\.\pipe\mz-gui-agentd-`) {
		t.Fatalf("Named Pipe 必须带 \\\\.\\pipe\\mz-gui-agentd- 前缀，实际 %q", got)
	}
	if got == `\\.\pipe\mz-gui-agentd` {
		t.Fatal("Named Pipe 必须包含 SID SHA-256 前缀，不得只用裸管道名")
	}
	if strings.Contains(got, "windows-arm") || strings.Contains(got, "arm64") {
		t.Fatal("Named Pipe 契约不得引入 windows-arm")
	}

	helper := PipeName("mz-gui-helper", "S-1-5-21-1000")
	if !strings.HasPrefix(helper, `\\.\pipe\mz-gui-helper-`) {
		t.Fatalf("helper 端点必须独立 Named Pipe，实际 %q", helper)
	}
	if helper == got {
		t.Fatal("helper 与 agentd 不得共用同一 Named Pipe")
	}
}
