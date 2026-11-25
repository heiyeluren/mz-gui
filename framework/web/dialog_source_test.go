package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitDialogSourceExposesModalContract(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "Dialog.tsx")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, "aria-describedby", "Escape", "onDismiss", "trapFocus", "rememberFocus", "previouslyFocused", "tabIndex", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(body, want) {
			t.Errorf("Dialog 缺少 %s", want)
		}
	}
}
