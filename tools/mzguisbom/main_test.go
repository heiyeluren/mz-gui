package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunPrintsDirectGoModules(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("生成 SBOM 失败: %d %s", code, stderr.String())
	}
	var doc struct {
		Format string `json:"format"`
		Source struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"source"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != "mzgui-sbom-lite" || doc.Source.Path != "go.mod" || len(doc.Components) == 0 {
		t.Fatalf("SBOM 输出无效: %s", stdout.String())
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(mod)
	if doc.Source.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("go.mod 摘要错误: %s", doc.Source.SHA256)
	}
	found := false
	for _, component := range doc.Components {
		if component.Name == "github.com/wailsapp/wails/v3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("缺少 wails 直接依赖: %s", stdout.String())
	}
}
