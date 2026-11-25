package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitThemeSourceDeclaresModesAndContrastGate(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "theme.ts")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"resolveTheme", "createThemeSnapshot", "validateBrandTokens", "high-contrast", "system"} {
		if !strings.Contains(body, want) {
			t.Errorf("theme 源码缺少 %s", want)
		}
	}
	tokens, err := os.ReadFile(filepath.Join(filepath.Dir(file), "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(tokens)
	if !strings.Contains(css, "prefers-contrast") || !strings.Contains(css, ":focus-visible") {
		t.Fatal("tokens.css 必须映射系统高对比并放大可见焦点")
	}
	if !strings.Contains(css, "--mzgui-space-4: 1rem") || !strings.Contains(css, "--mzgui-font-size-body: 1rem") {
		t.Fatal("tokens.css 必须用 rem 保证 200% 缩放")
	}
	dirSrc, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "dir.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dirSrc), "textDirection") || !strings.Contains(string(dirSrc), "rtl") {
		t.Fatal("必须提供 RTL 文本方向契约")
	}
	provider, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "ThemeProvider.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	spoken := string(provider)
	for _, want := range []string{"aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("ThemeProvider 缺少契约 %s", want)
		}
	}
}
