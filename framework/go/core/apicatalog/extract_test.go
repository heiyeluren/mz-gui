package apicatalog

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractDirFindsWindowStatePublicSymbols(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "interaction", "windowstate")
	symbols, err := ExtractDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Symbol{}
	for _, symbol := range symbols {
		found[symbol.Name] = symbol
	}
	for _, name := range []string{"NewMemoryStore", "OpenSQLiteStore", "MemoryStore.Save"} {
		symbol, ok := found[name]
		if !ok || symbol.SignatureSHA256 == "" || symbol.Package == "" {
			t.Fatalf("未抽到已发布符号 %s: %#v", name, symbol)
		}
	}
	if found["NewMemoryStore"].Kind != KindFunction || found["MemoryStore.Save"].Kind != KindMethod {
		t.Fatalf("符号类型错误: %#v", found)
	}
}

func TestExtractDirRejectsEmptyPath(t *testing.T) {
	if _, err := ExtractDir(""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}
