package main

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBootstrapMinimalNewAppRunsCandidatePath(t *testing.T) {
	dir := t.TempDir()
	service := &ShowcaseService{shellDir: dir}
	if err := service.prepareFrameworkShell(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapMinimalNewApp(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "logs", "showcase-desktop-*.jsonl"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("新 APP 必须写入本机日志: matches=%v err=%v", matches, err)
	}
}

func TestBootstrapMinimalNewAppRejectsInvalidInputs(t *testing.T) {
	if err := bootstrapMinimalNewApp(nil, t.TempDir()); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
	if err := bootstrapMinimalNewApp(context.Background(), ""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}

func TestShowcaseProductionFilesDoNotImportMoziApp(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "github.com/heiyeluren/mz-gui/apps/mozi" || strings.HasPrefix(path, "github.com/heiyeluren/mz-gui/apps/mozi/") {
				t.Fatalf("%s 禁止 import %s", name, path)
			}
		}
	}
	if _, err := os.Stat(filepath.Join("mzgui.app.toml")); err != nil {
		t.Fatalf("缺少新 APP manifest 模板: %v", err)
	}
}
