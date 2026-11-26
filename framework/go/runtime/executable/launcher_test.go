package executable

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/processrun"
)

func TestResolverResolvesNativeNodeWrapperAndShellShim(t *testing.T) {
	trusted := t.TempDir()
	cases := []struct {
		name string
		kind string
		body []byte
	}{
		{name: "native-cli", kind: LauncherNative, body: macho64Header()},
		{name: "wrapper-cli", kind: LauncherNodeWrapper, body: []byte("#!/usr/bin/env node\nconsole.log('ok')\n")},
		{name: "shim-cli", kind: LauncherShellShim, body: []byte("#!/bin/sh\nprintf 'ok\\n'\n")},
	}
	for _, test := range cases {
		path := filepath.Join(trusted, test.name)
		if err := os.WriteFile(path, test.body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNativeNode(t, trusted)

	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{trusted, "/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		resolved, resolveErr := resolver.Resolve(test.name)
		want := filepath.Join(trusted, test.name)
		if resolveErr != nil || resolved != want {
			t.Fatalf("%s Resolve 失败: path=%q err=%v", test.kind, resolved, resolveErr)
		}
		launch, launchErr := resolver.ResolveLaunch(test.name)
		if launchErr != nil {
			t.Fatalf("%s ResolveLaunch 失败: %v", test.kind, launchErr)
		}
		if launch.Path != want || launch.Kind != test.kind {
			t.Fatalf("%s 启动形态错误: %+v", test.kind, launch)
		}
		if test.kind == LauncherNodeWrapper && (launch.NodePath == "" || !launch.RequiresNode) {
			t.Fatalf("node-wrapper 必须解析到 node: %+v", launch)
		}
	}
}

func TestInspectClassifiesShebangAndSuffixLike0435(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		kind string
		body []byte
	}{
		{name: "macho", kind: LauncherNative, body: macho64Header()},
		{name: "elf", kind: LauncherNative, body: []byte{0x7f, 'E', 'L', 'F', 0x02}},
		{name: "env-node", kind: LauncherNodeWrapper, body: []byte("#!/usr/bin/env node\n")},
		{name: "abs-node", kind: LauncherNodeWrapper, body: []byte("#!/usr/bin/node\n")},
		{name: "plain.js", kind: LauncherNodeWrapper, body: []byte("console.log(1)\n")},
		{name: "env-bash", kind: LauncherShellShim, body: []byte("#!/usr/bin/env bash\n")},
		{name: "bin-sh", kind: LauncherShellShim, body: []byte("#!/bin/sh\n")},
		{name: "run.cmd", kind: LauncherShellShim, body: []byte("@echo off\r\n")},
	}
	for _, test := range cases {
		path := filepath.Join(root, test.name)
		if err := os.WriteFile(path, test.body, 0o755); err != nil {
			t.Fatal(err)
		}
		launch, err := Inspect(path)
		if err != nil || launch.Kind != test.kind || launch.Path != path {
			t.Fatalf("%s 分类失败: kind=%q err=%v", test.name, launch.Kind, err)
		}
	}

	python := filepath.Join(root, "tool")
	if err := os.WriteFile(python, []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(python); !errors.Is(err, ErrUnsupportedLauncher) {
		t.Fatalf("python shebang 应拒绝: %v", err)
	}
}

func TestLaunchFingerprintTracksLauncherAndNodeBinding(t *testing.T) {
	root := t.TempDir()
	wrapper := filepath.Join(root, "codex")
	nodeA := filepath.Join(root, "node-a")
	nodeB := filepath.Join(root, "node-b")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env node\nconsole.log('ok')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodeA, []byte("#!/bin/sh\nprintf node-a\\n\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodeB, []byte("#!/bin/sh\nprintf node-b\\n\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch, err := Inspect(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	launch.NodePath = nodeA
	first, err := LaunchFingerprint(launch)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("启动指纹必须是 sha256: %q", first)
	}
	again, err := LaunchFingerprint(launch)
	if err != nil || again != first {
		t.Fatalf("未变化的启动计划指纹必须稳定: first=%q again=%q err=%v", first, again, err)
	}
	launch.NodePath = nodeB
	second, err := LaunchFingerprint(launch)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("Node 绑定变化后启动指纹必须变化")
	}
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf shim\\n\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shimLaunch, err := Inspect(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	third, err := LaunchFingerprint(shimLaunch)
	if err != nil {
		t.Fatal(err)
	}
	if third == first || third == second {
		t.Fatal("launcher 形态变化后启动指纹必须变化")
	}
}

func TestLaunchStartsNativeNodeWrapperAndShellShim(t *testing.T) {
	if testing.Short() {
		t.Skip("启动验证需要本机 /bin/echo")
	}
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("/bin/echo 不存在")
	}
	resolvedEcho, err := filepath.EvalSymlinks("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	trusted := t.TempDir()
	native := filepath.Join(trusted, "native-cli")
	if err := os.Symlink("/bin/echo", native); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(trusted, "wrapper-cli")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(trusted, "shim-cli")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nprintf 'shim-ok\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(trusted, "node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\nprintf 'wrapper-ok\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{trusted, filepath.Dir(resolvedEcho)}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		kind   string
		output string
	}{
		{name: "native-cli", kind: LauncherNative, output: "native-ok"},
		{name: "wrapper-cli", kind: LauncherNodeWrapper, output: "wrapper-ok"},
		{name: "shim-cli", kind: LauncherShellShim, output: "shim-ok"},
	}
	for _, test := range cases {
		launch, launchErr := resolver.ResolveLaunch(test.name)
		if launchErr != nil || launch.Kind != test.kind {
			t.Fatalf("%s ResolveLaunch 失败: %+v err=%v", test.kind, launch, launchErr)
		}
		arguments := []string{}
		if test.kind == LauncherNative {
			arguments = []string{"native-ok"}
		}
		binary, args, applyErr := Apply(launch, arguments)
		if applyErr != nil {
			t.Fatalf("%s Apply 失败: %v", test.kind, applyErr)
		}
		result := processrun.Run(context.Background(), processrun.Spec{
			Executable: binary, Arguments: args,
			Environment: []string{"PATH=" + trusted}, Directory: trusted,
		})
		if result.Err != nil || result.ExitCode != 0 || !bytes.Contains(result.Output, []byte(test.output)) {
			t.Fatalf("%s 启动失败: exit=%d output=%q err=%v", test.kind, result.ExitCode, result.Output, result.Err)
		}
	}
}

func TestNodeWrapperUsesResolvedNodeInsteadOfProcessPath(t *testing.T) {
	trusted := t.TempDir()
	activeNodeBin := filepath.Join(trusted, "active-node", "bin")
	historicalNodeBin := filepath.Join(trusted, "historical-node", "bin")
	wrapper := filepath.Join(trusted, "codex")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeNodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(historicalNodeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	activeNode := filepath.Join(activeNodeBin, "node")
	if err := os.WriteFile(activeNode, []byte("#!/bin/sh\nprintf 'active-node %s\\n' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(historicalNodeBin, "node"), []byte("#!/bin/sh\nprintf 'historical-node %s\\n' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolver, err := NewResolver(Options{GOOS: "darwin", TrustedDirectories: []string{trusted, activeNodeBin}})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := resolver.ResolveLaunch("codex")
	if err != nil {
		t.Fatal(err)
	}
	binary, args, err := Apply(launch, []string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if binary != activeNode {
		t.Fatalf("node-wrapper 必须绑定 resolver 验证过的 node: binary=%q want=%q", binary, activeNode)
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX node-wrapper 脚本不能在 Windows 直接执行；ResolveLaunch 与 Apply 已验证")
	}
	result := processrun.Run(context.Background(), processrun.Spec{
		Executable: binary, Arguments: args,
		Environment: []string{"PATH=" + historicalNodeBin}, Directory: trusted,
	})
	if result.Err != nil || result.ExitCode != 0 || !bytes.Contains(result.Output, []byte("active-node "+wrapper)) {
		t.Fatalf("node-wrapper 不应退回进程 PATH 里的历史 node: exit=%d output=%q err=%v", result.ExitCode, result.Output, result.Err)
	}
}

func macho64Header() []byte {
	return []byte{0xcf, 0xfa, 0xed, 0xfe, 0x07, 0x00, 0x00, 0x01}
}

func writeNativeNode(t *testing.T, directory string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "node"), macho64Header(), 0o755); err != nil {
		t.Fatal(err)
	}
}
