package architecture_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/apicatalog"
)

type coreAPIManifest struct {
	SchemaVersion    int      `json:"schema_version"`
	ReleaseVersion   string   `json:"release_version"`
	APISchemaVersion string   `json:"api_schema_version"`
	Catalog          string   `json:"catalog"`
	Baseline         string   `json:"baseline"`
	CoreAPIIDs       []string `json:"core_api_ids"`
}

func TestCoreAPICompatibilityBaseline(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "framework", "api", "core-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest coreAPIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.ReleaseVersion != "0.0.4" || manifest.APISchemaVersion != "1.0.0" {
		t.Fatalf("核心 API 版本映射错误: %#v", manifest)
	}
	packageData, err := os.ReadFile(filepath.Join(root, "framework", "ui-kit", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var uiPackage struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageData, &uiPackage); err != nil {
		t.Fatal(err)
	}
	if uiPackage.Version != manifest.ReleaseVersion {
		t.Fatalf("UI Kit 版本 %s 与 release_version %s 不一致", uiPackage.Version, manifest.ReleaseVersion)
	}

	catalog := readAPICatalog(t, root, manifest.Catalog)
	readAPICatalog(t, root, manifest.Baseline)
	seen := make(map[string]struct{}, len(manifest.CoreAPIIDs))
	for _, id := range manifest.CoreAPIIDs {
		if _, exists := seen[id]; exists {
			t.Fatalf("核心 API 重复登记: %s", id)
		}
		seen[id] = struct{}{}
		if _, exists := catalog.Lookup(id); !exists {
			t.Errorf("核心 API 未进入公共 catalog: %s", id)
		}
	}
	if len(seen) == 0 {
		t.Fatal("核心 API 清单不得为空")
	}
}

func readAPICatalog(t *testing.T, root, relativePath string) apicatalog.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "framework", "api", relativePath))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := apicatalog.Decode(data)
	if err != nil {
		t.Fatalf("公共 API catalog 无效: %s: %v", relativePath, err)
	}
	return catalog
}
