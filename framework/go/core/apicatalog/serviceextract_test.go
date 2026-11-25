package apicatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractServiceMethodsFindsExportedServiceReceivers(t *testing.T) {
	dir := t.TempDir()
	source := `package demo
type SettingsService struct{}
func (s *SettingsService) Load() error { return nil }
func (s SettingsService) Save() {}
func (s *SettingsService) hidden() {}
type Other struct{}
func (o *Other) Ignore() {}
`
	if err := os.WriteFile(filepath.Join(dir, "service.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service_test.go"), []byte("package demo\nfunc (s *SettingsService) TestOnly() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symbols, err := ExtractServiceMethods(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Symbol{}
	for _, symbol := range symbols {
		found[symbol.Name] = symbol
	}
	if found["SettingsService.Load"].Kind != KindMethod || found["SettingsService.Save"].Kind != KindMethod {
		t.Fatalf("未抽到 Service 方法: %#v", found)
	}
	if _, ok := found["SettingsService.hidden"]; ok {
		t.Fatal("不得抽取未导出方法")
	}
	if _, ok := found["Other.Ignore"]; ok {
		t.Fatal("非 Service 接收者不得抽取")
	}
	if _, ok := found["SettingsService.TestOnly"]; ok {
		t.Fatal("测试文件不得抽取")
	}
}

func TestExtractServiceMethodsRejectsEmptyPath(t *testing.T) {
	if _, err := ExtractServiceMethods(""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}
