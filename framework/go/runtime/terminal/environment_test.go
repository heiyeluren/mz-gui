package terminal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProgramEnvironmentAcceptsComposedRuntimeWithinBoundedBudget(t *testing.T) {
	for _, count := range []int{32, 33, 128} {
		environment := make(map[string]string, count)
		for i := range count {
			environment[fmt.Sprintf("RUNTIME_%d", i)] = "fixture"
		}
		if !validProgramEnvironment(environment) {
			t.Fatalf("合法组合环境被拒绝: count=%d", count)
		}
	}
	if !validProgramEnvironment(map[string]string{"https_proxy": "http://127.0.0.1:8080", "SystemRoot": `C:\Windows`}) {
		t.Fatal("平台原生大小写环境名称必须可用")
	}
}

func TestManagerRejectsEnvironmentWithSafeDiagnosticBeforeLaunch(t *testing.T) {
	tooMany := map[string]string{}
	tooLarge := map[string]string{}
	for i := range 129 {
		tooMany[fmt.Sprintf("ENV_%d", i)] = "private-value"
	}
	for i := range 33 {
		tooLarge[fmt.Sprintf("ENV_%d", i)] = strings.Repeat("x", 4096)
	}
	for _, test := range []struct {
		name        string
		environment map[string]string
		message     string
	}{
		{"count", tooMany, "数量"},
		{"total", tooLarge, "总大小"},
		{"name", map[string]string{"private-name!": "private-value"}, "名称"},
		{"control", map[string]string{"PRIVATE": "private-value\n"}, "控制字符"},
		{"value-size", map[string]string{"PRIVATE": strings.Repeat("x", 4097)}, "单值"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			launcher := &fakeLauncher{}
			manager := newTestManager(t, root, launcher, 1)
			_, err := manager.Create(context.Background(), SessionSpec{
				CWD: root, ShellProfile: "system", Scope: "environment-test", Cols: 80, Rows: 24,
				Program: &ProgramSpec{Executable: filepath.Join(root, "fixture-agent"), Environment: test.environment},
			})
			if !errors.Is(err, ErrInvalidArgument) || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("缺少兼容且具体的环境诊断: %v", err)
			}
			if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), strings.Repeat("x", 20)) || launcher.count() != 0 {
				t.Fatalf("拒绝前不得启动进程或回显环境: %v", err)
			}
		})
	}
}

func TestPlatformLauncherTermProgramIsValidatedAndInjected(t *testing.T) {
	if _, err := normalizeTermProgram(PlatformLauncherOptions{TermProgram: "bad value"}); err == nil {
		t.Fatal("不安全的终端程序标识必须拒绝")
	}
	value, err := normalizeTermProgram(PlatformLauncherOptions{})
	if err != nil || value != "desktop-app" {
		t.Fatalf("默认终端程序标识错误: value=%q err=%v", value, err)
	}
	environment := allowedEnvironment("framework-showcase")
	if !slices.Contains(environment, "TERM_PROGRAM=framework-showcase") {
		t.Fatalf("终端程序标识未注入: %v", environment)
	}
}

func TestMergeProgramEnvironmentUsesValidatedOverrides(t *testing.T) {
	base := []string{"PATH=/usr/bin", "TERM=xterm-256color", "PYTHONUNBUFFERED=0"}
	merged := mergeProgramEnvironment(base, map[string]string{"PYTHONUNBUFFERED": "1", "PLAYWRIGHT_BROWSERS_PATH": "/managed/browsers"})
	joined := strings.Join(merged, "\n")
	if strings.Count(joined, "PYTHONUNBUFFERED=") != 1 || !strings.Contains(joined, "PYTHONUNBUFFERED=1") || !strings.Contains(joined, "PLAYWRIGHT_BROWSERS_PATH=/managed/browsers") {
		t.Fatalf("环境覆盖结果错误: %v", merged)
	}
	if validProgramEnvironment(map[string]string{"bad-key": "value"}) || validProgramEnvironment(map[string]string{"TOKEN": "line\nbreak"}) {
		t.Fatalf("非法程序环境变量未被拒绝")
	}
}
