package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitIconButtonRequiresAccessibleName(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src", "IconButton.tsx")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"aria-label", "disabled", "onPress"} {
		if !strings.Contains(body, want) {
			t.Errorf("IconButton 缺少 %s", want)
		}
	}
}
