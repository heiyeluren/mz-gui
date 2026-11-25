package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitVirtualListExposesListboxSemanticsAndKeyboard(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "VirtualList.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{`role="listbox"`, `role="option"`, "aria-selected", "aria-label", "aria-valuetext", "ArrowUp", "ArrowDown", "itemHeight", "slice", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("VirtualList 缺少契约 %s", want)
		}
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatal("VirtualList 空态必须使用 role=status")
	}
	if !strings.Contains(string(indexSrc), "VirtualList") {
		t.Fatal("UI Kit 入口必须导出 VirtualList")
	}
}
