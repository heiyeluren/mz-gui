package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitPageSelectAndLiveRegionContracts(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	pageState, err := os.ReadFile(filepath.Join(root, "pageState.ts"))
	if err != nil {
		t.Fatal(err)
	}
	selectSrc, err := os.ReadFile(filepath.Join(root, "Select.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	live, err := os.ReadFile(filepath.Join(root, "LiveRegion.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pageState), "restoreWindowPages") || !strings.Contains(string(pageState), "loadPage") {
		t.Fatal("pageState 缺少跨窗口页面恢复契约")
	}
	if !strings.Contains(string(selectSrc), "aria-label") || !strings.Contains(string(selectSrc), "aria-valuetext") || !strings.Contains(string(selectSrc), "<select") ||
		!strings.Contains(string(selectSrc), "aria-busy") || !strings.Contains(string(selectSrc), "voiceOverStateLabel('disabled')") || !strings.Contains(string(selectSrc), "voiceOverStateLabel('loading')") {
		t.Fatal("Select 缺少可访问下拉契约")
	}
	if !strings.Contains(string(live), `role="status"`) || !strings.Contains(string(live), "aria-live") {
		t.Fatal("LiveRegion 缺少屏幕阅读器契约")
	}
}
