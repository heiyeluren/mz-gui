package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitTreeExposesTreeSemantics(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	tree, err := os.ReadFile(filepath.Join(root, "Tree.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(tree)
	for _, want := range []string{`role="tree"`, `role="treeitem"`, "aria-expanded", "aria-selected", "aria-level", "aria-label", "ArrowDown", "ArrowRight", "voiceOverStateLabel('empty')", "voiceOverCountText", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Tree 缺少无障碍契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "Tree") {
		t.Fatal("UI Kit 入口必须导出 Tree")
	}
}
