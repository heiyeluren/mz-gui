package deviceprobe

import (
	"runtime"
	"testing"
)

func TestIdentifyReportsCurrentRuntime(t *testing.T) {
	report, err := Identify()
	if err != nil || report.GOOS != runtime.GOOS || report.GOARCH != runtime.GOARCH {
		t.Fatalf("平台报告错误: %#v %v", report, err)
	}
}
