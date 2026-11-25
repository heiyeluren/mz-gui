package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/core/requirements"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunValidatesRepositoryMatrixAndPrintsSummary(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("矩阵检查失败: code=%d stderr=%s", code, stderr.String())
	}
	var summary requirements.Summary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Total != 19 || summary.Implemented != 19 || summary.Partial != 0 || summary.Missing != 0 {
		t.Fatalf("汇总结果异常: %#v", summary)
	}
}

func TestRunWithoutGateFlagsStillPrintsSummary(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("默认检查必须退出 0: code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "implemented=19") {
		t.Fatalf("默认检查应打印汇总: stdout=%s", stdout.String())
	}
}

func TestRunKeepsIncompleteReleaseGateClosed(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--require-complete"}, &stdout, &stderr); code != 2 {
		t.Fatalf("当前缺口矩阵不得通过完整发布门禁: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunMacosArm64CompleteGatePassesCurrentMatrix(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--require-macos-arm64-complete", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("当前 macos-arm64 本机矩阵必须通过 Mac-first 门禁: code=%d stderr=%s", code, stderr.String())
	}
	var summary requirements.Summary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	platform := summary.Platforms["macos-arm64"]
	if platform.NotVerified != 0 || platform.CrossCompiled != 0 || platform.LocalAutomated != summary.Total {
		t.Fatalf("macos-arm64 汇总异常: %#v", platform)
	}
	if summary.Platforms["windows-x64"].CrossCompiled == 0 {
		t.Fatal("Windows 交叉编译基线被改掉了，不得为了 Mac-first 门禁假装三平台 complete")
	}
}

func TestRunMacosArm64CompleteGateRejectsCrossCompiledMacArm(t *testing.T) {
	root := repoRoot(t)
	original, err := os.ReadFile(filepath.Join(root, "framework", "requirements", "v1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	patched := bytes.Replace(original, []byte(`"macos-arm64": "local-automated"`), []byte(`"macos-arm64": "cross-compiled"`), 1)
	if bytes.Equal(patched, original) {
		t.Fatal("未能把 macos-arm64 改成 cross-compiled")
	}
	matrixPath := filepath.Join(t.TempDir(), "v1.0.json")
	if err := os.WriteFile(matrixPath, patched, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--matrix", matrixPath, "--require-macos-arm64-complete"}, &stdout, &stderr); code != 2 {
		t.Fatalf("macos-arm64 交叉编译必须挡住 Mac-first 门禁: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "macos-arm64") {
		t.Fatalf("Mac-first 门禁失败信息应点名 macos-arm64: stderr=%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--root", root, "--matrix", matrixPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("无门禁 flag 时临时矩阵仍只打印汇总: code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunRejectsMissingMatrix(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", t.TempDir()}, &stdout, &stderr); code != 1 {
		t.Fatalf("缺少矩阵时必须失败: code=%d", code)
	}
}

func TestRunReportsMacosX8664StillCrossCompiled(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("默认检查必须退出 0: code=%d stderr=%s", code, stderr.String())
	}
	var summary requirements.Summary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	platform := summary.Platforms["macos-x86_64"]
	if platform.LocalAutomated != 0 || platform.CrossCompiled != summary.Total {
		t.Fatalf("macos-x86_64 必须保持交叉编译基线: %#v", platform)
	}
	if _, exists := summary.Platforms["windows-arm64"]; exists {
		t.Fatal("frameworkcheck 汇总不得出现 windows-arm64")
	}
}

func TestRunMacosX8664CompleteGateStaysClosed(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := run([]string{"--root", root, "--require-macos-x86_64-complete"}, &stdout, &stderr); code != 2 {
		t.Fatalf("macos-x86_64 交叉编译不得通过 Intel 完成门禁: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "macos-x86_64") {
		t.Fatalf("Intel 门禁失败信息应点名 macos-x86_64: stderr=%s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--root", root, "--require-macos-arm64-complete"}, &stdout, &stderr); code != 0 {
		t.Fatalf("G610 不得挡住 macos-arm64 Mac-first 门禁: code=%d stderr=%s", code, stderr.String())
	}
}
