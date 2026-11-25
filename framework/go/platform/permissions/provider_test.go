package permissions

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/security/leakscan"
)

func TestProviderProbeMatrixReturnsStableStatesAndDeniedGuidance(t *testing.T) {
	capabilities := []string{Microphone, Accessibility, Automation, Notifications}
	states := []string{Granted, Denied, Unsupported}
	for _, capability := range capabilities {
		for _, state := range states {
			provider := NewProvider(ProviderOptions{
				ApplicationID: "com.example.sample",
				States:        map[string]string{capability: state},
			})
			result, err := provider.Probe(context.Background(), capability)
			if err != nil {
				t.Fatalf("%s/%s: %v", capability, state, err)
			}
			if result.Capability != capability || result.State != state {
				t.Fatalf("%s/%s 探测结果无效: %#v", capability, state, result)
			}
			if state != Denied {
				if result.Guidance != nil {
					t.Fatalf("%s/%s 不应返回设置引导: %#v", capability, state, result.Guidance)
				}
				continue
			}
			if result.Guidance == nil || result.Guidance.SettingsURL == "" || result.Guidance.Message == "" {
				t.Fatalf("%s denied 缺少设置引导: %#v", capability, result.Guidance)
			}
			if !strings.HasPrefix(result.Guidance.SettingsURL, "x-apple.systempreferences:") && !strings.HasPrefix(result.Guidance.SettingsURL, "ms-settings:") {
				t.Fatalf("%s 设置 URL 非法: %q", capability, result.Guidance.SettingsURL)
			}
			blob := result.Guidance.SettingsURL + "\n" + result.Guidance.Message
			if findings := leakscan.Scan(blob); len(findings) != 0 {
				t.Fatalf("%s 引导文案含敏感模式: %#v text=%q", capability, findings, blob)
			}
			lower := strings.ToLower(blob)
			for _, forbidden := range []string{"token", "secret", "bearer", "authorization", "refresh_token", "app_secret"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s 引导文案不得包含 %q: %q", capability, forbidden, blob)
				}
			}
		}
	}
}

func TestProviderProbeAllUsesInjectedContractStates(t *testing.T) {
	provider := NewProvider(ProviderOptions{States: map[string]string{
		Microphone:    Granted,
		Accessibility: Denied,
		Automation:    Unsupported,
		Notifications: Denied,
	}})
	results, err := provider.ProbeAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("capability 数量=%d", len(results))
	}
	want := map[string]string{
		Microphone: Granted, Accessibility: Denied, Automation: Unsupported, Notifications: Denied,
	}
	for _, result := range results {
		if result.State != want[result.Capability] {
			t.Fatalf("%s 状态=%q", result.Capability, result.State)
		}
		if result.State == Denied && (result.Guidance == nil || result.Guidance.SettingsURL == "" || result.Guidance.Message == "") {
			t.Fatalf("%s denied 缺少引导: %#v", result.Capability, result)
		}
		if result.State != Denied && result.Guidance != nil {
			t.Fatalf("%s 非 denied 不应有引导: %#v", result.Capability, result)
		}
	}
}

func TestProviderMapsNativeSystemStatusToContractStates(t *testing.T) {
	cases := []struct {
		system string
		want   string
		guide  bool
	}{
		{Authorized, Granted, false},
		{Denied, Denied, true},
		{Restricted, Denied, true},
		{NotDetermined, Denied, true},
		{Unavailable, Unsupported, false},
		{ManagedBySystem, Unsupported, false},
	}
	for _, item := range cases {
		provider := NewProvider(ProviderOptions{
			Observe: func(context.Context, string) (string, error) {
				return item.system, nil
			},
		})
		result, err := provider.Probe(context.Background(), Microphone)
		if err != nil {
			t.Fatalf("%s: %v", item.system, err)
		}
		if result.State != item.want {
			t.Fatalf("system=%s 映射为 %q, 期望 %q", item.system, result.State, item.want)
		}
		if item.guide && result.Guidance == nil {
			t.Fatalf("system=%s 应有引导", item.system)
		}
		if !item.guide && result.Guidance != nil {
			t.Fatalf("system=%s 不应有引导: %#v", item.system, result.Guidance)
		}
	}
}

func TestProviderRejectsInvalidCapabilityStateAndContext(t *testing.T) {
	provider := NewProvider(ProviderOptions{States: map[string]string{Microphone: Granted}})
	if _, err := provider.Probe(context.Background(), "camera"); err == nil {
		t.Fatal("未知 capability 必须拒绝")
	}
	if _, err := provider.Probe(nil, Microphone); err == nil {
		t.Fatal("nil context 必须拒绝")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Probe(ctx, Microphone); err == nil {
		t.Fatal("已取消上下文必须返回错误")
	}
	invalid := NewProvider(ProviderOptions{States: map[string]string{Microphone: Authorized}})
	if _, err := invalid.Probe(context.Background(), Microphone); err == nil {
		t.Fatal("非 granted/denied/unsupported 必须拒绝")
	}
}

func TestProviderLiveProbeReadsCurrentValuesWithoutPrompting(t *testing.T) {
	provider := NewProvider(ProviderOptions{ApplicationID: "com.example.sample"})
	results, err := provider.ProbeAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("capability 数量=%d", len(results))
	}
	allowed := map[string]bool{Granted: true, Denied: true, Unsupported: true}
	for _, result := range results {
		if !allowed[result.State] {
			t.Fatalf("真机探测状态必须是 granted/denied/unsupported: %#v", result)
		}
		if result.State == Denied {
			if result.Guidance == nil || result.Guidance.SettingsURL == "" || result.Guidance.Message == "" {
				t.Fatalf("真机 denied 缺少引导: %#v", result)
			}
			blob := result.Guidance.SettingsURL + "\n" + result.Guidance.Message
			if findings := leakscan.Scan(blob); len(findings) != 0 {
				t.Fatalf("真机引导含敏感模式: %#v", findings)
			}
			if strings.Contains(strings.ToLower(blob), "token") {
				t.Fatalf("真机引导含 token: %q", blob)
			}
		}
	}
}

func TestDarwinNativeProbeSourceNeverRequestsAuthorizationOrVoiceOver(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件")
	}
	nativePath := filepath.Join(filepath.Dir(filename), "native_darwin.m")
	payload, err := os.ReadFile(nativePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	for _, forbidden := range []string{
		"requestAccessForMediaType",
		"AXIsProcessTrustedWithOptions",
		"kAXTrustedCheckOptionPrompt",
		"CGRequestScreenCaptureAccess",
		"requestAuthorizationWithOptions",
		"askUserIfNeeded:YES",
		"askUserIfNeeded:true",
		"VoiceOver",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("native 探测不得触发授权弹窗或 VoiceOver：含 %q", forbidden)
		}
	}
	if !strings.Contains(source, "gui_permission_automation_status") {
		t.Fatal("native 探测必须覆盖 automation capability")
	}
	if !strings.Contains(source, "askUserIfNeeded") && !strings.Contains(source, "false") {
		t.Fatal("automation 探测必须显式关闭用户询问")
	}
}

func TestSettingsURIIncludesAutomation(t *testing.T) {
	value, ok := SettingsURI(Automation, SettingsOptions{ApplicationID: "com.example.sample"})
	if !ok || value == "" {
		t.Fatal("automation 缺少设置 URI")
	}
}
