package apicatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTypeScriptExportsFindsNamedExports(t *testing.T) {
	dir := t.TempDir()
	source := "export { Checkbox, Switch } from './Checkbox'\nexport type { CheckboxProps } from './Checkbox'\n"
	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols, err := ExtractTypeScriptDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Symbol{}
	for _, symbol := range symbols {
		found[symbol.Name] = symbol
	}
	if found["Checkbox"].Kind != KindFunction || found["Switch"].Kind != KindFunction {
		t.Fatalf("未抽到组件导出: %#v", found)
	}
	if found["CheckboxProps"].Kind != KindType {
		t.Fatalf("未抽到类型导出: %#v", found)
	}
}

func TestExtractTypeScriptDirRejectsEmptyPath(t *testing.T) {
	if _, err := ExtractTypeScriptDir(""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}

func TestExtractTypeScriptDirReadsUIKitIndex(t *testing.T) {
	root := projectRoot(t)
	symbols, err := ExtractTypeScriptDir(filepath.Join(root, "framework", "ui-kit", "src"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]struct{}{}
	for _, symbol := range symbols {
		found[symbol.Name] = struct{}{}
	}
	for _, name := range []string{"Checkbox", "Switch", "Select", "LiveRegion"} {
		if _, ok := found[name]; !ok {
			t.Fatalf("UI Kit 入口缺少导出 %s", name)
		}
	}
}
