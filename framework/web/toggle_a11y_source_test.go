package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitCheckboxAndSwitchExposeAccessibleNames(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	checkbox, err := os.ReadFile(filepath.Join(root, "Checkbox.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	switchSrc, err := os.ReadFile(filepath.Join(root, "Switch.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checkbox), `type="checkbox"`) || !strings.Contains(string(checkbox), "aria-label") {
		t.Fatal("Checkbox 缺少可访问勾选契约")
	}
	if !strings.Contains(string(switchSrc), `role="switch"`) || !strings.Contains(string(switchSrc), "aria-checked") || !strings.Contains(string(switchSrc), "aria-label") {
		t.Fatal("Switch 缺少屏幕阅读器开关契约")
	}
	if !strings.Contains(string(indexSrc), "Checkbox") || !strings.Contains(string(indexSrc), "Switch") {
		t.Fatal("UI Kit 入口必须导出 Checkbox 与 Switch")
	}
}
