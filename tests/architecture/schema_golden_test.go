package architecture_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigAndSchemaGolden(t *testing.T) {
	root := repositoryRoot(t)
	goldenPath := filepath.Join(root, "framework", "api", "golden", "schema-v1.json")
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("读取 schema golden: %v", err)
	}
	var golden struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"framework/api/core-v1.schema.json",
		"framework/api/public-v1.schema.json",
		"framework/api/core-v1.json",
		"framework/api/public-v1.json",
		"framework/api/caller-usage/framework-showcase.json",
		"framework/api/caller-usage/framework-plugin-echo.json",
		"framework/appmanifest/mzgui.app.schema.json",
		"framework/capabilities/catalog.json",
		"examples/framework-showcase/mzgui.app.toml",
		"examples/framework-agentd-showcase/mzgui.app.toml",
	}
	if len(golden.Files) != len(paths) {
		t.Fatalf("golden 文件数量=%d，要求=%d", len(golden.Files), len(paths))
	}
	for _, relativePath := range paths {
		content, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		canonical := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		actual := fmt.Sprintf("%x", sha256.Sum256(canonical))
		if golden.Files[relativePath] != actual {
			t.Errorf("%s 与 golden 不一致；确认兼容影响后同步更新 golden", relativePath)
		}
	}
}
