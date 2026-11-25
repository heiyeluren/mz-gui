package diagnostics

import (
	"strings"
	"testing"
)

func TestFingerprintCrashIsStableAndDistinct(t *testing.T) {
	first, err := FingerprintCrash("panic", "desktop", "goroutine 1 [running]:\nmain.main()")
	if err != nil || !strings.HasPrefix(first, "c") || len(first) < 16 {
		t.Fatalf("指纹无效: %s err=%v", first, err)
	}
	second, err := FingerprintCrash("panic", "desktop", "goroutine 1 [running]:\nmain.main()")
	if err != nil || first != second {
		t.Fatalf("相同输入必须稳定: %s vs %s err=%v", first, second, err)
	}
	other, err := FingerprintCrash("panic", "agentd", "goroutine 1 [running]:\nmain.main()")
	if err != nil || other == first {
		t.Fatal("不同组件必须产生不同指纹")
	}
	if _, err := FingerprintCrash("", "desktop", "stack"); err == nil {
		t.Fatal("空错误类必须拒绝")
	}
}
