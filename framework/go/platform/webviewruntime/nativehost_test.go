package webviewruntime

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestPlanNativeWindowRejectsInvalidSpec(t *testing.T) {
	valid := NativeWindowSpec{
		WindowID:       "main-window",
		Title:          "MZ GUI",
		Width:          1280,
		Height:         800,
		InternalOrigin: "app://desktop",
		MinimumVersion: "1.0.0",
	}
	for _, test := range []struct {
		name string
		spec NativeWindowSpec
	}{
		{name: "empty window", spec: NativeWindowSpec{Title: valid.Title, Width: 1280, Height: 800, InternalOrigin: valid.InternalOrigin}},
		{name: "empty title", spec: NativeWindowSpec{WindowID: valid.WindowID, Width: 1280, Height: 800, InternalOrigin: valid.InternalOrigin}},
		{name: "zero size", spec: NativeWindowSpec{WindowID: valid.WindowID, Title: valid.Title, InternalOrigin: valid.InternalOrigin}},
		{name: "missing origin", spec: NativeWindowSpec{WindowID: valid.WindowID, Title: valid.Title, Width: 1280, Height: 800}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PlanNativeWindow(test.spec); !errors.Is(err, ErrNativeWindowSpec) {
				t.Fatalf("非法规格必须拒绝: %v", err)
			}
		})
	}
}

func TestPlanNativeWindowBindsWKWebViewOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("WKWebView 原生窗口仅在 macOS 本机探测")
	}
	plan, err := PlanNativeWindow(NativeWindowSpec{
		WindowID:       "main-window",
		Title:          "MZ GUI",
		Width:          1280,
		Height:         800,
		InternalOrigin: "app://desktop",
		MinimumVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Engine != EngineWKWebView || !plan.Native || plan.WindowID != "main-window" {
		t.Fatalf("必须绑定 WKWebView 原生窗口: %#v", plan)
	}
	if plan.Width != 1280 || plan.Height != 800 || plan.Title != "MZ GUI" {
		t.Fatalf("窗口几何必须保留: %#v", plan)
	}
	if strings.TrimSpace(plan.Version) == "" || plan.VersionClass != VersionOK {
		t.Fatalf("本机 WebKit 版本必须可分级: %#v", plan)
	}
	if plan.Security.CSPHeader == "" || plan.Security.DevToolsEnabled {
		t.Fatalf("原生窗口必须带生产安全配置: %#v", plan.Security)
	}
}

func TestNormalizeNativeVersionPadsAndTruncates(t *testing.T) {
	if got := normalizeNativeVersion("21624"); got != "21624.0.0" {
		t.Fatalf("短版本应补齐: %q", got)
	}
	if got := normalizeNativeVersion("21624.2.5.11.4"); got != "21624.2.5.11" {
		t.Fatalf("长版本应截到 4 段: %q", got)
	}
}
