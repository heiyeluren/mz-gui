package artifact

import "testing"

func TestParseGoModRequiresDirectModules(t *testing.T) {
	components, err := ParseGoModRequires([]byte(`
module github.com/heiyeluren/mz-gui

go 1.25.0

require (
	github.com/wailsapp/wails/v3 v3.0.0-alpha2.119
	modernc.org/sqlite v1.56.0
)

require (
	github.com/indirect/lib v1.2.3 // indirect
)
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("应只收集直接依赖: %#v", components)
	}
	if components[0].Name != "github.com/wailsapp/wails/v3" || components[0].Version != "v3.0.0-alpha2.119" || components[0].Type != "library" {
		t.Fatalf("第一项错误: %#v", components[0])
	}
	if components[1].Name != "modernc.org/sqlite" || components[1].Version != "v1.56.0" {
		t.Fatalf("第二项错误: %#v", components[1])
	}
}

func TestParseGoModRejectsEmptyModule(t *testing.T) {
	if _, err := ParseGoModRequires(nil); err == nil {
		t.Fatal("空 go.mod 必须拒绝")
	}
}
