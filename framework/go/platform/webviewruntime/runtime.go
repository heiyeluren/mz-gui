package webviewruntime

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/security/webpolicy"
)

var versionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){3}$`)

type Spec struct {
	Executable string
	Version    string
	Arch       string
}

type SecuritySpec struct {
	InternalOrigin         string
	Environment            webpolicy.Environment
	NavigationRules        []webpolicy.URLRule
	ResourceRules          []webpolicy.URLRule
	EnableDevTools         bool
	EnableRemoteDebugging  bool
	RemoteDebuggingAddress string
	RemoteDebuggingPort    int
}

type SecurityConfig struct {
	Policy                 webpolicy.Policy
	CSPHeader              string
	DevToolsEnabled        bool
	RemoteDebuggingEnabled bool
	RemoteDebuggingAddress string
	RemoteDebuggingPort    int
}

type CrashReason string

const (
	CrashReasonRendererProcessGone CrashReason = "renderer_process_gone"
	CrashReasonGPUProcessGone      CrashReason = "gpu_process_gone"
	CrashReasonPageReloadHung      CrashReason = "page_reload_hung"
)

type RecoveryAction string

const (
	RecoveryActionRecreateWebView         RecoveryAction = "recreate_webview"
	RecoveryActionEnableSoftwareRendering RecoveryAction = "enable_software_rendering"
	RecoveryActionEnterDiagnosticMode     RecoveryAction = "enter_diagnostic_mode"
)

type RecoveryPolicy struct {
	WindowID               string
	CrashWindow            time.Duration
	SoftwareRenderingAfter int
	DiagnosticAfter        int
	RecreateDelay          time.Duration
}

type CrashEvent struct {
	WindowID   string
	Reason     CrashReason
	OccurredAt time.Time
}

type RecoveryDecision struct {
	WindowID               string
	Action                 RecoveryAction
	Reason                 CrashReason
	Attempt                int
	OccurredAt             time.Time
	PreserveWindowIdentity bool
	UseStartupSnapshot     bool
	SoftwareRendering      bool
	DiagnosticMode         bool
	DisableAnimation       bool
	RecreateWebViewAllowed bool
	NextDelay              time.Duration
	ConsecutiveCrashWindow time.Duration
	ConsecutiveCrashCount  int
}

type RecoveryState struct {
	mu              sync.Mutex
	policy          RecoveryPolicy
	crashes         []CrashEvent
	softwareEnabled bool
	diagnosticMode  bool
	lastStableAt    time.Time
}

// Resolve 返回与桌面程序同级且通过完整性检查的固定版 WebView2 目录。
func Resolve(spec Spec) (string, error) {
	executable := filepath.Clean(spec.Executable)
	if !filepath.IsAbs(executable) || isUNC(executable) {
		return "", errors.New("桌面程序路径必须是本地绝对路径")
	}
	if err := validateRegularFile(executable); err != nil {
		return "", fmt.Errorf("校验桌面程序: %w", err)
	}
	version := strings.TrimSpace(spec.Version)
	if version != spec.Version || !versionPattern.MatchString(version) {
		return "", errors.New("WebView2 固定版本无效")
	}
	if _, _, err := architecture(spec.Arch); err != nil {
		return "", err
	}

	runtimeRoot := filepath.Join(filepath.Dir(executable), "webview2-runtime")
	runtimeDirectory := filepath.Join(runtimeRoot, version)
	if err := validateDirectory(runtimeRoot); err != nil {
		return "", fmt.Errorf("校验 WebView2 根目录: %w", err)
	}
	if err := validateDirectory(runtimeDirectory); err != nil {
		return "", fmt.Errorf("校验 WebView2 版本目录: %w", err)
	}
	if err := ensureContained(runtimeRoot, runtimeDirectory); err != nil {
		return "", err
	}

	if err := ValidateDirectory(runtimeDirectory, version, spec.Arch); err != nil {
		return "", err
	}
	return runtimeDirectory, nil
}

func BuildSecurityConfig(spec SecuritySpec) (SecurityConfig, error) {
	environment := spec.Environment
	if environment == "" {
		environment = webpolicy.EnvironmentProduction
	}
	devtools := webpolicy.DevToolsPolicy{}
	if spec.EnableDevTools {
		devtools = webpolicy.DevToolsPolicy{
			Enabled:             true,
			AllowedEnvironments: []webpolicy.Environment{webpolicy.EnvironmentDevelopment},
		}
	}
	policy, err := webpolicy.NewStrictPolicy(webpolicy.Options{
		InternalOrigin:  spec.InternalOrigin,
		NavigationRules: spec.NavigationRules,
		ResourceRules:   spec.ResourceRules,
		DevTools:        devtools,
	})
	if err != nil {
		return SecurityConfig{}, err
	}
	cspHeader, err := policy.CSPHeader()
	if err != nil {
		return SecurityConfig{}, err
	}
	devToolsEnabled := policy.AllowsDevTools(environment)
	remoteDebuggingEnabled := false
	address := ""
	port := 0
	if devToolsEnabled && spec.EnableRemoteDebugging {
		if err := validateRemoteDebuggingEndpoint(spec.RemoteDebuggingAddress, spec.RemoteDebuggingPort); err != nil {
			return SecurityConfig{}, err
		}
		remoteDebuggingEnabled = true
		address = spec.RemoteDebuggingAddress
		port = spec.RemoteDebuggingPort
	}
	return SecurityConfig{
		Policy:                 policy,
		CSPHeader:              cspHeader,
		DevToolsEnabled:        devToolsEnabled,
		RemoteDebuggingEnabled: remoteDebuggingEnabled,
		RemoteDebuggingAddress: address,
		RemoteDebuggingPort:    port,
	}, nil
}

func NewRecoveryState(policy RecoveryPolicy) (*RecoveryState, error) {
	if strings.TrimSpace(policy.WindowID) == "" {
		return nil, errors.New("WebView 窗口身份不能为空")
	}
	if policy.CrashWindow <= 0 {
		return nil, errors.New("WebView 崩溃窗口必须大于 0")
	}
	if policy.SoftwareRenderingAfter <= 0 || policy.DiagnosticAfter <= 0 || policy.SoftwareRenderingAfter > policy.DiagnosticAfter {
		return nil, errors.New("WebView 崩溃恢复阈值无效")
	}
	return &RecoveryState{policy: policy}, nil
}

func (state *RecoveryState) RecordCrash(event CrashEvent) (RecoveryDecision, error) {
	if state == nil {
		return RecoveryDecision{}, errors.New("WebView 恢复状态不能为空")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if event.WindowID != state.policy.WindowID || event.OccurredAt.IsZero() || event.Reason == "" {
		return RecoveryDecision{}, errors.New("WebView 崩溃事件无效")
	}
	state.prune(event.OccurredAt)
	state.crashes = append(state.crashes, event)
	attempt := len(state.crashes)
	action := RecoveryActionRecreateWebView
	if attempt >= state.policy.DiagnosticAfter {
		state.softwareEnabled = true
		state.diagnosticMode = true
		action = RecoveryActionEnterDiagnosticMode
	} else if attempt >= state.policy.SoftwareRenderingAfter {
		state.softwareEnabled = true
		action = RecoveryActionEnableSoftwareRendering
	}
	return RecoveryDecision{
		WindowID:               state.policy.WindowID,
		Action:                 action,
		Reason:                 event.Reason,
		Attempt:                attempt,
		OccurredAt:             event.OccurredAt.UTC(),
		PreserveWindowIdentity: true,
		UseStartupSnapshot:     true,
		SoftwareRendering:      state.softwareEnabled,
		DiagnosticMode:         state.diagnosticMode,
		DisableAnimation:       state.softwareEnabled,
		RecreateWebViewAllowed: !state.diagnosticMode,
		NextDelay:              state.policy.RecreateDelay,
		ConsecutiveCrashWindow: state.policy.CrashWindow,
		ConsecutiveCrashCount:  attempt,
	}, nil
}

func (state *RecoveryState) MarkStable(at time.Time) {
	if state == nil || at.IsZero() {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.lastStableAt = at
	state.crashes = nil
	state.softwareEnabled = false
	state.diagnosticMode = false
}

func (state *RecoveryState) Snapshot() RecoveryDecision {
	if state == nil {
		return RecoveryDecision{}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return RecoveryDecision{
		WindowID:               state.policy.WindowID,
		Action:                 RecoveryActionRecreateWebView,
		Attempt:                len(state.crashes),
		PreserveWindowIdentity: true,
		UseStartupSnapshot:     true,
		SoftwareRendering:      state.softwareEnabled,
		DiagnosticMode:         state.diagnosticMode,
		DisableAnimation:       state.softwareEnabled,
		RecreateWebViewAllowed: !state.diagnosticMode,
		NextDelay:              state.policy.RecreateDelay,
		ConsecutiveCrashWindow: state.policy.CrashWindow,
		ConsecutiveCrashCount:  len(state.crashes),
	}
}

func (state *RecoveryState) prune(now time.Time) {
	threshold := now.Add(-state.policy.CrashWindow)
	index := 0
	for _, event := range state.crashes {
		if !event.OccurredAt.Before(threshold) && (state.lastStableAt.IsZero() || event.OccurredAt.After(state.lastStableAt)) {
			state.crashes[index] = event
			index++
		}
	}
	state.crashes = state.crashes[:index]
	if index == 0 {
		state.softwareEnabled = false
		state.diagnosticMode = false
	}
}

// ValidateDirectory 校验一个已经定位的 Fixed Version Runtime 目录。
func ValidateDirectory(runtimeDirectory, version, arch string) error {
	if !filepath.IsAbs(runtimeDirectory) || isUNC(runtimeDirectory) {
		return errors.New("WebView2 目录必须是本地绝对路径")
	}
	if strings.TrimSpace(version) != version || !versionPattern.MatchString(version) {
		return errors.New("WebView2 固定版本无效")
	}
	machine, dllArch, err := architecture(arch)
	if err != nil {
		return err
	}
	if err := validateDirectory(runtimeDirectory); err != nil {
		return fmt.Errorf("校验 WebView2 版本目录: %w", err)
	}
	required := []string{
		"msedgewebview2.exe",
		"msedge.dll",
		"icudtl.dat",
		"resources.pak",
		version + ".manifest",
		filepath.Join("EBWebView", dllArch, "EmbeddedBrowserWebView.dll"),
	}
	for _, name := range required {
		path := filepath.Join(runtimeDirectory, name)
		if err := validateRegularFile(path); err != nil {
			return fmt.Errorf("校验 WebView2 关键文件 %s: %w", name, err)
		}
		if err := ensureContained(runtimeDirectory, path); err != nil {
			return fmt.Errorf("校验 WebView2 关键文件 %s: %w", name, err)
		}
	}
	actualMachine, err := readPEMachine(filepath.Join(runtimeDirectory, "msedgewebview2.exe"))
	if err != nil {
		return fmt.Errorf("校验 WebView2 PE: %w", err)
	}
	if actualMachine != machine {
		return fmt.Errorf("WebView2 架构不匹配: machine=0x%04x arch=%s", actualMachine, arch)
	}
	return nil
}

func validateRemoteDebuggingEndpoint(address string, port int) error {
	if port < 1024 || port > 65535 {
		return errors.New("远程调试端口无效")
	}
	ip := net.ParseIP(address)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("远程调试只能监听本机回环地址")
	}
	return nil
}

func architecture(arch string) (uint16, string, error) {
	switch arch {
	case "amd64":
		return 0x8664, "x64", nil
	case "arm64":
		return 0xAA64, "arm64", nil
	default:
		return 0, "", fmt.Errorf("不支持的 WebView2 架构 %q", arch)
	}
}

func validateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("必须是目录且不能是符号链接")
	}
	return nil
}

func validateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("必须是普通文件且不能是符号链接")
	}
	if info.Size() <= 0 {
		return errors.New("文件不能为空")
	}
	return nil
}

func ensureContained(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("解析 WebView2 根目录: %w", err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("解析 WebView2 版本目录: %w", err)
	}
	relative, err := filepath.Rel(realRoot, realTarget)
	if err != nil {
		return fmt.Errorf("比较 WebView2 目录: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("WebView2 版本目录越出固定根目录")
	}
	return nil
}

func readPEMachine(path string) (uint16, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	dosHeader := make([]byte, 64)
	if _, err := io.ReadFull(file, dosHeader); err != nil {
		return 0, errors.New("DOS 头不完整")
	}
	if dosHeader[0] != 'M' || dosHeader[1] != 'Z' {
		return 0, errors.New("DOS 签名无效")
	}
	peOffset := int64(binary.LittleEndian.Uint32(dosHeader[0x3c:]))
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if peOffset < 64 || peOffset > info.Size()-6 || peOffset > 16<<20 {
		return 0, errors.New("PE 头偏移无效")
	}
	if _, err := file.Seek(peOffset, io.SeekStart); err != nil {
		return 0, err
	}
	header := make([]byte, 6)
	if _, err := io.ReadFull(file, header); err != nil {
		return 0, errors.New("PE 头不完整")
	}
	if string(header[:4]) != "PE\x00\x00" {
		return 0, errors.New("PE 签名无效")
	}
	return binary.LittleEndian.Uint16(header[4:]), nil
}

func isUNC(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`)
}
