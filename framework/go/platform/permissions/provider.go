package permissions

import (
	"context"
	"errors"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/security/leakscan"
)

// ObserveFunc 返回 capability 的本机系统状态，不得弹出授权框。
type ObserveFunc func(ctx context.Context, capability string) (string, error)

type ProviderOptions struct {
	ApplicationID string
	Observe       ObserveFunc
	States        map[string]string
}

type Guidance struct {
	SettingsURL string
	Message     string
}

type ProbeResult struct {
	Capability string
	State      string
	Guidance   *Guidance
}

// Provider 以可注入探测提供 granted/denied/unsupported 契约。
type Provider struct {
	options ProviderOptions
}

func NewProvider(options ProviderOptions) *Provider {
	return &Provider{options: options}
}

func ContractCapabilities() []string {
	return []string{Microphone, Accessibility, Automation, Notifications}
}

func (provider *Provider) Probe(ctx context.Context, capability string) (ProbeResult, error) {
	if ctx == nil {
		return ProbeResult{}, errors.New("权限查询上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	if !isContractCapability(capability) {
		return ProbeResult{}, errors.New("系统权限标识无效")
	}
	state, err := provider.contractState(ctx, capability)
	if err != nil {
		return ProbeResult{}, err
	}
	result := ProbeResult{Capability: capability, State: state}
	if state != Denied {
		return result, nil
	}
	guidance, err := provider.deniedGuidance(capability)
	if err != nil {
		return ProbeResult{}, err
	}
	result.Guidance = guidance
	return result, nil
}

func (provider *Provider) ProbeAll(ctx context.Context) ([]ProbeResult, error) {
	ids := ContractCapabilities()
	results := make([]ProbeResult, 0, len(ids))
	for _, id := range ids {
		result, err := provider.Probe(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (provider *Provider) contractState(ctx context.Context, capability string) (string, error) {
	if provider != nil && provider.options.States != nil {
		state := strings.TrimSpace(provider.options.States[capability])
		if state == "" {
			state = Unsupported
		}
		if !isContractState(state) {
			return "", errors.New("权限探测状态无效")
		}
		return state, nil
	}
	systemStatus, err := provider.systemStatus(ctx, capability)
	if err != nil {
		return "", err
	}
	return mapSystemStatus(systemStatus), nil
}

func (provider *Provider) systemStatus(ctx context.Context, capability string) (string, error) {
	if provider != nil && provider.options.Observe != nil {
		return provider.options.Observe(ctx, capability)
	}
	return platformSystemStatus(capability)
}

func (provider *Provider) deniedGuidance(capability string) (*Guidance, error) {
	applicationID := ""
	if provider != nil {
		applicationID = provider.options.ApplicationID
	}
	uri, ok := SettingsURI(capability, SettingsOptions{ApplicationID: applicationID})
	if !ok || strings.TrimSpace(uri) == "" {
		return nil, errors.New("系统权限设置入口无效")
	}
	message := deniedGuidanceMessage(capability)
	if message == "" {
		return nil, errors.New("系统权限引导文案无效")
	}
	guidance := &Guidance{SettingsURL: uri, Message: message}
	if err := rejectSensitiveGuidance(guidance); err != nil {
		return nil, err
	}
	return guidance, nil
}

func mapSystemStatus(systemStatus string) string {
	switch systemStatus {
	case Authorized:
		return Granted
	case Denied, Restricted, NotDetermined:
		return Denied
	default:
		return Unsupported
	}
}

func isContractCapability(capability string) bool {
	switch capability {
	case Microphone, Accessibility, Automation, Notifications:
		return true
	default:
		return false
	}
}

func isContractState(state string) bool {
	switch state {
	case Granted, Denied, Unsupported:
		return true
	default:
		return false
	}
}

func deniedGuidanceMessage(capability string) string {
	switch capability {
	case Microphone:
		return "麦克风未授权。请在系统设置的隐私与安全性中允许麦克风。"
	case Accessibility:
		return "辅助功能未授权。请在系统设置的隐私与安全性中允许辅助功能。"
	case Automation:
		return "自动化未授权。请在系统设置的隐私与安全性中允许自动化。"
	case Notifications:
		return "通知未授权。请在系统设置中允许通知。"
	default:
		return ""
	}
}

func rejectSensitiveGuidance(guidance *Guidance) error {
	if guidance == nil {
		return errors.New("系统权限引导文案无效")
	}
	blob := guidance.SettingsURL + "\n" + guidance.Message
	if findings := leakscan.Scan(blob); len(findings) != 0 {
		return errors.New("系统权限引导不得包含凭据")
	}
	lower := strings.ToLower(blob)
	for _, forbidden := range []string{"token", "secret", "bearer", "authorization"} {
		if strings.Contains(lower, forbidden) {
			return errors.New("系统权限引导不得包含凭据")
		}
	}
	return nil
}
