package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitBreadcrumbExposesNavSemantics(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "Breadcrumb.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{"<nav", "<ol", `aria-current="page"`, "voiceOverStateLabel('empty')", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Breadcrumb 缺少无障碍契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "Breadcrumb") {
		t.Fatal("UI Kit 入口必须导出 Breadcrumb")
	}
}
