package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitFormFeedbackSourceUsesAccessibleContracts(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	field, err := os.ReadFile(filepath.Join(root, "TextField.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	toast, err := os.ReadFile(filepath.Join(root, "FeedbackToast.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"htmlFor", "aria-invalid", "aria-required", "aria-describedby", "aria-live"} {
		if !strings.Contains(string(field), want) {
			t.Errorf("TextField 缺少 %s", want)
		}
	}
	for _, want := range []string{`role={role}`, "success", "error", "onDismiss", "aria-busy", "voiceOverStateLabel('disabled')", "voiceOverStateLabel('loading')"} {
		if !strings.Contains(string(toast), want) {
			t.Errorf("FeedbackToast 缺少 %s", want)
		}
	}
}
