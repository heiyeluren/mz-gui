package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitWindowStateHookUsesInjectedStore(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "windowState.ts")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"useWindowState", "store.load", "store.save", "revision"} {
		if !strings.Contains(body, want) {
			t.Errorf("windowState hook 缺少 %s", want)
		}
	}
}
