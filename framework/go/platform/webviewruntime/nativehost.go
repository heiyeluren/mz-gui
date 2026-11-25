package webviewruntime

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrNativeWindowSpec      = errors.New("原生窗口规格无效")
	ErrNativeHostUnavailable = errors.New("本机原生 WebView 不可用")
)

type EngineKind string

const (
	EngineWKWebView EngineKind = "wkwebview"
	EngineWebView2  EngineKind = "webview2"
)

type NativeWindowSpec struct {
	WindowID       string
	Title          string
	Width          int
	Height         int
	InternalOrigin string
	MinimumVersion string
}

type NativeWindowPlan struct {
	Engine       EngineKind
	WindowID     string
	Title        string
	Width        int
	Height       int
	Version      string
	VersionClass VersionClass
	Native       bool
	Security     SecurityConfig
}

type engineInfo struct {
	Engine    EngineKind
	Version   string
	Available bool
}

func PlanNativeWindow(spec NativeWindowSpec) (NativeWindowPlan, error) {
	if err := validateNativeWindowSpec(spec); err != nil {
		return NativeWindowPlan{}, err
	}
	info, err := probeNativeEngine()
	if err != nil {
		return NativeWindowPlan{}, err
	}
	if !info.Available || info.Engine == "" || strings.TrimSpace(info.Version) == "" {
		return NativeWindowPlan{}, ErrNativeHostUnavailable
	}
	version := normalizeNativeVersion(info.Version)
	class := VersionOK
	if minimum := strings.TrimSpace(spec.MinimumVersion); minimum != "" {
		class, err = ClassifyVersion(version, normalizeNativeVersion(minimum))
		if err != nil {
			return NativeWindowPlan{}, err
		}
	}
	security, err := BuildSecurityConfig(SecuritySpec{InternalOrigin: spec.InternalOrigin})
	if err != nil {
		return NativeWindowPlan{}, err
	}
	return NativeWindowPlan{
		Engine:       info.Engine,
		WindowID:     strings.TrimSpace(spec.WindowID),
		Title:        strings.TrimSpace(spec.Title),
		Width:        spec.Width,
		Height:       spec.Height,
		Version:      version,
		VersionClass: class,
		Native:       true,
		Security:     security,
	}, nil
}

func validateNativeWindowSpec(spec NativeWindowSpec) error {
	windowID := strings.TrimSpace(spec.WindowID)
	title := strings.TrimSpace(spec.Title)
	if windowID == "" || strings.ContainsFunc(windowID, unicode.IsSpace) {
		return ErrNativeWindowSpec
	}
	if title == "" || strings.ContainsFunc(title, unicode.IsControl) {
		return ErrNativeWindowSpec
	}
	if spec.Width < 1 || spec.Width > 8192 || spec.Height < 1 || spec.Height > 8192 {
		return ErrNativeWindowSpec
	}
	if strings.TrimSpace(spec.InternalOrigin) == "" {
		return ErrNativeWindowSpec
	}
	return nil
}

func normalizeNativeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	parts := strings.Split(value, ".")
	normalized := make([]string, 0, 4)
	for _, part := range parts {
		if part == "" {
			continue
		}
		if _, err := strconv.Atoi(part); err != nil {
			return value
		}
		normalized = append(normalized, part)
		if len(normalized) == 4 {
			break
		}
	}
	for len(normalized) < 3 {
		normalized = append(normalized, "0")
	}
	return strings.Join(normalized, ".")
}
