package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitMediaSurfaceAllowsSafeSourcesAndCanCancel(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "MediaSurface.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	if strings.Contains(body, "dangerouslySetInnerHTML") {
		t.Fatal("MediaSurface 不得拼接未净化 HTML")
	}
	for _, want := range []string{"https://", "javascript:", "preload", "aria-label", "aria-valuetext", "<audio", "<video", "cancelMedia"} {
		if !strings.Contains(body, want) {
			t.Fatalf("MediaSurface 缺少契约 %s", want)
		}
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatal("MediaSurface 非法源必须降级为 status 空态")
	}
	if !strings.Contains(string(indexSrc), "MediaSurface") {
		t.Fatal("UI Kit 入口必须导出 MediaSurface")
	}
}
