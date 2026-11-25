package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFrameworkDoesNotImportApplicationCode(t *testing.T) {
	root := repositoryRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "framework"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			imports, ok := declaration.(*ast.GenDecl)
			if !ok || imports.Tok != token.IMPORT {
				continue
			}
			for _, spec := range imports.Specs {
				pathValue, err := strconv.Unquote(spec.(*ast.ImportSpec).Path.Value)
				if err != nil {
					return err
				}
				if strings.Contains(pathValue, "/apps/") || strings.HasSuffix(pathValue, "/apps") {
					t.Errorf("公共 Framework 不得导入产品应用包: %s -> %s", path, pathValue)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicContractsDoNotRequireMoziConsumer(t *testing.T) {
	root := repositoryRoot(t)
	for _, relativePath := range []string{
		"framework/api/public-v1.json",
		"framework/requirements/v1.0.json",
	} {
		content, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"mozi-desktop", "apps/mozi"} {
			if strings.Contains(string(content), forbidden) {
				t.Errorf("独立公共契约不得依赖 %q: %s", forbidden, relativePath)
			}
		}
	}
}

func TestProductionSourcesDoNotContainPrivateOrInternalLocations(t *testing.T) {
	root := repositoryRoot(t)
	for _, directory := range []string{"framework", "examples", "gen", "tools"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"/Users/black", "gitlab.xiaoluxue.cn", "172.27."} {
				if strings.Contains(string(content), forbidden) {
					t.Errorf("公共生产源码不得包含内部位置 %q: %s", forbidden, path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
