package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitProgressBarExposesProgressSemantics(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "ProgressBar.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{`role="progressbar"`, "aria-valuemin", "aria-valuemax", "aria-valuenow", "aria-valuetext", "aria-label"} {
		if !strings.Contains(body, want) {
			t.Fatalf("ProgressBar 缺少无障碍契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "ProgressBar") {
		t.Fatal("UI Kit 入口必须导出 ProgressBar")
	}
}
