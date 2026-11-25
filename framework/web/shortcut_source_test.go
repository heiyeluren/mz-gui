package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitShortcutSourceMatchesPlatformSemantics(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	source, err := os.ReadFile(filepath.Join(root, "shortcut.ts"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := os.ReadFile(filepath.Join(root, "ShortcutListener.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	for _, want := range []string{"matchShortcut", "cmd-or-ctrl", "macos", "windows"} {
		if !strings.Contains(body, want) {
			t.Errorf("shortcut 源码缺少 %s", want)
		}
	}
	spoken := string(listener)
	for _, want := range []string{"aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(spoken, want) {
			t.Errorf("ShortcutListener 缺少契约 %s", want)
		}
	}
}
