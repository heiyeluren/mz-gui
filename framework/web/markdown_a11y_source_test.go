package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitMarkdownViewerUsesAllowlistAndNoRawHTML(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "MarkdownViewer.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if strings.Contains(body, "dangerouslySetInnerHTML") {
		t.Fatal("MarkdownViewer 不得拼接未净化 HTML")
	}
	for _, want := range []string{`role="article"`, "aria-label", "https://", "javascript:", "parseMarkdown", "renderInline", "renderAutoInlineLinks", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(body, want) {
			t.Fatalf("MarkdownViewer 缺少契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "MarkdownViewer") {
		t.Fatal("UI Kit 入口必须导出 MarkdownViewer")
	}
}
