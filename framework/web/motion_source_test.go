package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitMotionDegradesOnReducedSignals(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "motion.ts"))
	if err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile(filepath.Join(filepath.Dir(file), "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{"resolveMotionPreference", "prefersReducedMotion", "resourcePressure", "remoteDesktop", "reduced"} {
		if !strings.Contains(body, want) {
			t.Fatalf("motion 缺少契约 %s", want)
		}
	}
	if !strings.Contains(string(css), "prefers-reduced-motion") {
		t.Fatal("tokens.css 必须声明 prefers-reduced-motion 降级")
	}
	if !strings.Contains(string(indexSrc), "resolveMotionPreference") {
		t.Fatal("UI Kit 入口必须导出 resolveMotionPreference")
	}
}
