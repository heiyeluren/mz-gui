package webviewruntime

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/diagnostics"
	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

const testVersion = "151.0.4129.86"

func TestResolveAcceptsMatchingFixedRuntime(t *testing.T) {
	for _, test := range []struct {
		arch    string
		machine uint16
		dllArch string
	}{
		{arch: "amd64", machine: 0x8664, dllArch: "x64"},
		{arch: "arm64", machine: 0xAA64, dllArch: "arm64"},
	} {
		t.Run(test.arch, func(t *testing.T) {
			executable, runtimeDirectory := createRuntimeFixture(t, test.machine, test.dllArch)
			resolved, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: test.arch})
			if err != nil {
				t.Fatal(err)
			}
			if resolved != runtimeDirectory || !filepath.IsAbs(resolved) {
				t.Fatalf("resolved=%q want=%q", resolved, runtimeDirectory)
			}
		})
	}
}

func TestResolveRejectsInvalidInputs(t *testing.T) {
	executable, _ := createRuntimeFixture(t, 0x8664, "x64")
	for _, test := range []struct {
		name string
		spec Spec
	}{
		{name: "relative executable", spec: Spec{Executable: "mz-gui-desktop.exe", Version: testVersion, Arch: "amd64"}},
		{name: "unc executable", spec: Spec{Executable: `\\server\share\mz-gui-desktop.exe`, Version: testVersion, Arch: "amd64"}},
		{name: "empty version", spec: Spec{Executable: executable, Arch: "amd64"}},
		{name: "path version", spec: Spec{Executable: executable, Version: "../151.0.0.0", Arch: "amd64"}},
		{name: "unknown arch", spec: Spec{Executable: executable, Version: testVersion, Arch: "386"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Resolve(test.spec); err == nil {
				t.Fatal("必须拒绝无效参数")
			}
		})
	}
}

func TestResolveRejectsIncompleteOrUntrustedRuntime(t *testing.T) {
	t.Run("missing key file", func(t *testing.T) {
		executable, runtimeDirectory := createRuntimeFixture(t, 0x8664, "x64")
		if err := os.Remove(filepath.Join(runtimeDirectory, "resources.pak")); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"}); err == nil {
			t.Fatal("缺少关键文件必须拒绝")
		}
	})

	t.Run("empty key file", func(t *testing.T) {
		executable, runtimeDirectory := createRuntimeFixture(t, 0x8664, "x64")
		if err := os.WriteFile(filepath.Join(runtimeDirectory, "msedge.dll"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"}); err == nil {
			t.Fatal("空关键文件必须拒绝")
		}
	})

	t.Run("symlink key file", func(t *testing.T) {
		testkit.RequireSymlink(t)
		executable, runtimeDirectory := createRuntimeFixture(t, 0x8664, "x64")
		target := filepath.Join(filepath.Dir(runtimeDirectory), "outside.dll")
		if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(runtimeDirectory, "msedge.dll")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"}); err == nil {
			t.Fatal("符号链接关键文件必须拒绝")
		}
	})

	t.Run("nested directory escape", func(t *testing.T) {
		testkit.RequireSymlink(t)
		executable, runtimeDirectory := createRuntimeFixture(t, 0x8664, "x64")
		outside := filepath.Join(filepath.Dir(runtimeDirectory), "outside-ebwebview")
		if err := os.MkdirAll(filepath.Join(outside, "x64"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "x64", "EmbeddedBrowserWebView.dll"), []byte("outside"), 0o600); err != nil {
			t.Fatal(err)
		}
		ebWebView := filepath.Join(runtimeDirectory, "EBWebView")
		if err := os.RemoveAll(ebWebView); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, ebWebView); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"}); err == nil {
			t.Fatal("嵌套目录越界必须拒绝")
		}
	})

	t.Run("architecture mismatch", func(t *testing.T) {
		executable, _ := createRuntimeFixture(t, 0xAA64, "x64")
		_, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"})
		if err == nil || !strings.Contains(err.Error(), "架构") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("broken pe", func(t *testing.T) {
		executable, runtimeDirectory := createRuntimeFixture(t, 0x8664, "x64")
		if err := os.WriteFile(filepath.Join(runtimeDirectory, "msedgewebview2.exe"), []byte("broken"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve(Spec{Executable: executable, Version: testVersion, Arch: "amd64"}); err == nil {
			t.Fatal("损坏 PE 必须拒绝")
		}
	})
}

func TestBuildSecurityConfigAppliesStrictWebPolicy(t *testing.T) {
	config, err := BuildSecurityConfig(SecuritySpec{
		InternalOrigin: "app://framework",
		Environment:    webpolicy.EnvironmentProduction,
		NavigationRules: []webpolicy.URLRule{
			{ID: "docs", Scheme: "https", Host: "docs.example.com"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.CSPHeader == "" || !strings.Contains(config.CSPHeader, "default-src 'self'") {
		t.Fatalf("CSP header 未生成安全默认值: %#v", config)
	}
	if config.DevToolsEnabled || config.RemoteDebuggingEnabled {
		t.Fatalf("生产环境不得启用 DevTools 或远程调试: %#v", config)
	}
	if decision := config.Policy.EvaluateNavigation("app://framework/index.html"); !decision.Allowed {
		t.Fatalf("内部 origin 应允许导航: %#v", decision)
	}
	if decision := config.Policy.EvaluateNavigation("https://docs.example.com/start"); !decision.Allowed {
		t.Fatalf("显式外部导航白名单应允许: %#v", decision)
	}
	if decision := config.Policy.EvaluateNavigation("javascript:alert(1)"); decision.Allowed {
		t.Fatalf("危险导航 scheme 必须拒绝: %#v", decision)
	}
}

func TestBuildSecurityConfigRequiresExplicitDevelopmentForDebugging(t *testing.T) {
	production, err := BuildSecurityConfig(SecuritySpec{
		InternalOrigin:         "app://framework",
		Environment:            webpolicy.EnvironmentProduction,
		EnableDevTools:         true,
		EnableRemoteDebugging:  true,
		RemoteDebuggingPort:    9222,
		RemoteDebuggingAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if production.DevToolsEnabled || production.RemoteDebuggingEnabled {
		t.Fatalf("生产环境必须忽略调试开关: %#v", production)
	}

	development, err := BuildSecurityConfig(SecuritySpec{
		InternalOrigin:         "app://framework",
		Environment:            webpolicy.EnvironmentDevelopment,
		EnableDevTools:         true,
		EnableRemoteDebugging:  true,
		RemoteDebuggingPort:    9222,
		RemoteDebuggingAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !development.DevToolsEnabled || !development.RemoteDebuggingEnabled {
		t.Fatalf("开发环境显式开启时应允许调试: %#v", development)
	}
}

func TestBuildSecurityConfigRejectsUnsafeDebuggingInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		spec SecuritySpec
	}{
		{name: "missing origin", spec: SecuritySpec{Environment: webpolicy.EnvironmentDevelopment}},
		{name: "wildcard host", spec: SecuritySpec{
			InternalOrigin: "app://framework",
			Environment:    webpolicy.EnvironmentDevelopment,
			NavigationRules: []webpolicy.URLRule{
				{ID: "wildcard", Scheme: "https", Host: "*"},
			},
		}},
		{name: "remote address all interfaces", spec: SecuritySpec{
			InternalOrigin:         "app://framework",
			Environment:            webpolicy.EnvironmentDevelopment,
			EnableDevTools:         true,
			EnableRemoteDebugging:  true,
			RemoteDebuggingPort:    9222,
			RemoteDebuggingAddress: "0.0.0.0",
		}},
		{name: "remote port out of range", spec: SecuritySpec{
			InternalOrigin:         "app://framework",
			Environment:            webpolicy.EnvironmentDevelopment,
			EnableDevTools:         true,
			EnableRemoteDebugging:  true,
			RemoteDebuggingPort:    70000,
			RemoteDebuggingAddress: "127.0.0.1",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildSecurityConfig(test.spec); err == nil {
				t.Fatal("必须拒绝不安全 WebView 调试或导航配置")
			}
		})
	}
}

func TestRecoveryStateRecreatesWebViewAndPreservesWindowIdentity(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 3,
		DiagnosticAfter:        5,
		RecreateDelay:          200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	decision, err := state.RecordCrash(CrashEvent{
		WindowID:   "main-window",
		Reason:     CrashReasonRendererProcessGone,
		OccurredAt: time.Unix(1000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != RecoveryActionRecreateWebView || decision.WindowID != "main-window" || decision.Attempt != 1 {
		t.Fatalf("首次崩溃应重建同一窗口 WebView: %#v", decision)
	}
	if !decision.PreserveWindowIdentity || !decision.UseStartupSnapshot || decision.SoftwareRendering || decision.DiagnosticMode {
		t.Fatalf("首次崩溃恢复策略不正确: %#v", decision)
	}
	if decision.NextDelay != 200*time.Millisecond {
		t.Fatalf("恢复延迟不正确: %#v", decision)
	}
}

func TestRecoveryStateEscalatesContinuousCrashes(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 3,
		DiagnosticAfter:        5,
	})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Unix(2000, 0)
	var decision RecoveryDecision
	for index := 0; index < 5; index++ {
		decision, err = state.RecordCrash(CrashEvent{
			WindowID:   "main-window",
			Reason:     CrashReasonGPUProcessGone,
			OccurredAt: base.Add(time.Duration(index) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		if index == 2 && (decision.Action != RecoveryActionEnableSoftwareRendering || !decision.SoftwareRendering || decision.DiagnosticMode) {
			t.Fatalf("第三次连续崩溃应进入 software rendering: %#v", decision)
		}
	}
	if decision.Action != RecoveryActionEnterDiagnosticMode || !decision.SoftwareRendering || !decision.DiagnosticMode {
		t.Fatalf("连续崩溃达到上限必须进入诊断模式，不能无限重建: %#v", decision)
	}
}

func TestRecoveryStateStablePeriodResetsCrashWindow(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 2,
		DiagnosticAfter:        4,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(3000, 0)
	for index := 0; index < 2; index++ {
		if _, err := state.RecordCrash(CrashEvent{WindowID: "main-window", Reason: CrashReasonRendererProcessGone, OccurredAt: base.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	state.MarkStable(base.Add(2 * time.Minute))

	decision, err := state.RecordCrash(CrashEvent{
		WindowID:   "main-window",
		Reason:     CrashReasonPageReloadHung,
		OccurredAt: base.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Attempt != 1 || decision.Action != RecoveryActionRecreateWebView || decision.SoftwareRendering || decision.DiagnosticMode {
		t.Fatalf("稳定运行后应重置连续崩溃窗口: %#v", decision)
	}
}

func TestRecoveryStateRejectsInvalidPolicyAndMismatchedWindow(t *testing.T) {
	if _, err := NewRecoveryState(RecoveryPolicy{WindowID: "", CrashWindow: time.Minute, SoftwareRenderingAfter: 2, DiagnosticAfter: 4}); err == nil {
		t.Fatal("缺少窗口身份必须拒绝")
	}
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 2,
		DiagnosticAfter:        4,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.RecordCrash(CrashEvent{
		WindowID:   "secondary-window",
		Reason:     CrashReasonRendererProcessGone,
		OccurredAt: time.Unix(4000, 0),
	})
	if err == nil {
		t.Fatal("错误窗口来源的崩溃事件必须拒绝")
	}
}

func TestRecoveryStateConcurrentCrashAndSnapshot(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            10 * time.Second,
		SoftwareRenderingAfter: 4,
		DiagnosticAfter:        8,
	})
	if err != nil {
		t.Fatal(err)
	}

	base := time.Unix(5000, 0)
	done := make(chan struct{}, 3)
	for worker := 0; worker < 3; worker++ {
		worker := worker
		go func() {
			defer func() { done <- struct{}{} }()
			for index := 0; index < 16; index++ {
				_, _ = state.RecordCrash(CrashEvent{
					WindowID:   "main-window",
					Reason:     CrashReasonRendererProcessGone,
					OccurredAt: base.Add(time.Duration(worker*100+index) * time.Millisecond),
				})
				_ = state.Snapshot()
			}
		}()
	}
	for worker := 0; worker < 3; worker++ {
		<-done
	}

	decision := state.Snapshot()
	if decision.ConsecutiveCrashCount == 0 || !decision.DiagnosticMode || decision.RecreateWebViewAllowed {
		t.Fatalf("并发崩溃事件应得到一致的诊断模式快照: %#v", decision)
	}
}

func TestRecoveryDecisionWritesCrashSpoolRecord(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 2,
		DiagnosticAfter:        3,
		RecreateDelay:          250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := state.RecordCrash(CrashEvent{
		WindowID:   "main-window",
		Reason:     CrashReasonRendererProcessGone,
		OccurredAt: time.Unix(7000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	spool, err := diagnostics.NewCrashSpool(diagnostics.CrashSpoolOptions{
		Directory: t.TempDir(), MaxRecords: 4, MaxRecordBytes: 4096,
		Now: func() time.Time { return time.Unix(7001, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := WriteRecoveryDecision(context.Background(), spool, decision, RecoveryDiagnosticSpec{
		ProcessRole:   "desktop",
		Platform:      "macos-arm64",
		BuildID:       "test-build",
		SystemSummary: "/Users/black/private-token.txt",
		StackSummary:  "WKWebView content process exited",
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := diagnostics.ReplayCrashSpool(context.Background(), filepath.Dir(result.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("应回读一条 WebView 崩溃恢复记录，实际=%d", len(records))
	}
	record := records[0]
	if record.Fingerprint == "" || record.ProcessRole != "desktop" || record.Platform != "macos-arm64" || record.Component != "webview" {
		t.Fatalf("崩溃记录基础字段错误: %#v", record)
	}
	if record.ErrorClass != "webview_crash" || record.ReasonCode != string(CrashReasonRendererProcessGone) || record.OccurredAt.Unix() != 7000 {
		t.Fatalf("崩溃记录分类或时间错误: %#v", record)
	}
	if !strings.Contains(record.WebViewSummary, "recreate_webview") || !strings.Contains(record.WebViewSummary, "attempt=1") {
		t.Fatalf("恢复决策摘要缺失: %q", record.WebViewSummary)
	}
	if strings.Contains(record.SystemSummary, "/Users/black/private-token.txt") {
		t.Fatalf("诊断记录不得泄露本机私有路径: %#v", record)
	}
	if len(record.Breadcrumbs) != 1 || record.Breadcrumbs[0].Name != "webview.recovery.decision" {
		t.Fatalf("恢复决策 breadcrumb 缺失: %#v", record.Breadcrumbs)
	}
}

func TestRecoverySnapshotCannotFabricateCrashSpoolRecord(t *testing.T) {
	state, err := NewRecoveryState(RecoveryPolicy{
		WindowID:               "main-window",
		CrashWindow:            time.Minute,
		SoftwareRenderingAfter: 2,
		DiagnosticAfter:        4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CrashRecordFromRecoveryDecision(state.Snapshot(), RecoveryDiagnosticSpec{}); err == nil {
		t.Fatal("没有真实崩溃事件时间的 Snapshot 不得写入 crash spool")
	}
}

func createRuntimeFixture(t *testing.T, machine uint16, dllArch string) (string, string) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "mz-gui-desktop.exe")
	if err := os.WriteFile(executable, []byte("desktop"), 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeDirectory := filepath.Join(root, "webview2-runtime", testVersion)
	if err := os.MkdirAll(filepath.Join(runtimeDirectory, "EBWebView", dllArch), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"msedge.dll", "icudtl.dat", "resources.pak", testVersion + ".manifest"} {
		if err := os.WriteFile(filepath.Join(runtimeDirectory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeDirectory, "EBWebView", dllArch, "EmbeddedBrowserWebView.dll"), []byte("dll"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 96)
	data[0], data[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(data[0x3c:], 0x40)
	copy(data[0x40:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(data[0x44:], machine)
	if err := os.WriteFile(filepath.Join(runtimeDirectory, "msedgewebview2.exe"), data, 0o700); err != nil {
		t.Fatal(err)
	}
	return executable, runtimeDirectory
}
