package webviewruntime

import (
	"errors"
	"testing"
)

func TestClassifyVersionAcceptsCurrentAndRejectsOutdated(t *testing.T) {
	status, err := ClassifyVersion("120.0.2210.91", "109.0.1518.78")
	if err != nil || status != VersionOK {
		t.Fatalf("更高 WebView2 版本应为 ok: %s %v", status, err)
	}
	status, err = ClassifyVersion("90.0.0.0", "109.0.1518.78")
	if err != nil || status != VersionOutdated {
		t.Fatalf("更低 WebView2 版本应为 outdated: %s %v", status, err)
	}
	status, err = ClassifyVersion("1.2.3", "1.2.4")
	if err != nil || status != VersionOutdated {
		t.Fatalf("更低语义版本应为 outdated: %s %v", status, err)
	}
	if _, err := ClassifyVersion("bad", "1.0.0"); !errors.Is(err, ErrInvalidRuntimeVersion) {
		t.Fatalf("非法版本必须拒绝: %v", err)
	}
}
