package webassets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUIKitDataTableExposesTableSemantics(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "ui-kit", "src")
	table, err := os.ReadFile(filepath.Join(root, "DataTable.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	indexSrc, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(table)
	for _, want := range []string{"<table", "<caption", `scope="col"`, "aria-sort", "speakVoiceOverSort", "aria-valuetext"} {
		if !strings.Contains(body, want) {
			t.Fatalf("DataTable 缺少无障碍契约 %s", want)
		}
	}
	if !strings.Contains(string(indexSrc), "DataTable") {
		t.Fatal("UI Kit 入口必须导出 DataTable")
	}
}
