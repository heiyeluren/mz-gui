package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitTabsAndI18nSourcesExist(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	tabs, err := os.ReadFile(filepath.Join(root, "Tabs.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	i18n, err := os.ReadFile(filepath.Join(root, "i18n.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(tabs)
	if !strings.Contains(body, `role="tablist"`) || !strings.Contains(string(i18n), "formatMessage") {
		t.Fatal("Tabs/i18n 源码契约缺失")
	}
	for _, want := range []string{"ArrowRight", "ArrowLeft", "tabIndex"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Tabs 缺少键盘契约 %s", want)
		}
	}
}
