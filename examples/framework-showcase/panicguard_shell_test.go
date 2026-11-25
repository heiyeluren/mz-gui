package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProduceShowcaseShellReadyConfirmsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := produceShowcaseShellReady(context.Background(), dir); err == nil {
		t.Fatal("配置不存在时 Produce 必须失败")
	}
	if err := os.WriteFile(filepath.Join(dir, showcaseShellConfigName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := produceShowcaseShellReady(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestProduceShowcaseShellReadyRejectsInvalidInputs(t *testing.T) {
	if err := produceShowcaseShellReady(nil, t.TempDir()); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
	if err := produceShowcaseShellReady(context.Background(), ""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}

func TestProduceShowcaseShellConfigPathReturnsExistingFile(t *testing.T) {
	dir := t.TempDir()
	service := &ShowcaseService{shellDir: dir}
	if err := service.prepareFrameworkShell(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := produceShowcaseShellConfigPath(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, showcaseShellConfigName)
	if path != want {
		t.Fatalf("配置路径不符: got=%q want=%q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
}

func TestProduceShowcaseShellConfigPathRejectsMissingFile(t *testing.T) {
	if _, err := produceShowcaseShellConfigPath(context.Background(), t.TempDir()); err == nil {
		t.Fatal("缺少配置文件必须失败")
	}
}

func TestGuardShowcaseShellReadyConfirmsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := guardShowcaseShellReady(context.Background(), dir); err == nil {
		t.Fatal("配置不存在时 Guard 必须失败")
	}
	if err := os.WriteFile(filepath.Join(dir, showcaseShellConfigName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := guardShowcaseShellReady(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := guardShowcaseShellRequest(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
}

func TestGuardShowcaseShellReadyRejectsInvalidInputs(t *testing.T) {
	if err := guardShowcaseShellReady(nil, t.TempDir()); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
	if err := guardShowcaseShellReady(context.Background(), ""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
	if err := guardShowcaseShellRequest(nil, t.TempDir()); err == nil {
		t.Fatal("空上下文必须拒绝")
	}
	if err := guardShowcaseShellRequest(context.Background(), ""); err == nil {
		t.Fatal("空目录必须拒绝")
	}
}
