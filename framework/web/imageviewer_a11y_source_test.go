package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitImageViewerAllowsSafeSourcesOnly(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "ImageViewer.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if strings.Contains(body, "dangerouslySetInnerHTML") {
		t.Fatal("ImageViewer 不得拼接未净化 HTML")
	}
	for _, want := range []string{`role="img"`, "aria-label", "aria-valuetext", "https://", "javascript:", "data:image", "loading="} {
		if !strings.Contains(body, want) {
			t.Fatalf("ImageViewer 缺少契约 %s", want)
		}
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatal("ImageViewer 非法源必须降级为 status 空态")
	}
	if !strings.Contains(string(indexSrc), "ImageViewer") {
		t.Fatal("UI Kit 入口必须导出 ImageViewer")
	}
}
