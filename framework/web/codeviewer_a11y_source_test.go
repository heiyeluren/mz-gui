package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitCodeViewerIsPlainTextRegion(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "CodeViewer.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if strings.Contains(body, "dangerouslySetInnerHTML") {
		t.Fatal("CodeViewer 不得拼接未净化 HTML")
	}
	for _, want := range []string{`role="region"`, "aria-label", "aria-valuetext", "<code>", "language", "voiceOverStateLabel('empty')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("CodeViewer 缺少契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "CodeViewer") {
		t.Fatal("UI Kit 入口必须导出 CodeViewer")
	}
}
