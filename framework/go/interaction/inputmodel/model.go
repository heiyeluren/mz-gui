package inputmodel

import (
	"errors"
	"strings"
	"time"
)

type InputSource string

const (
	SourceMouse         InputSource = "mouse"
	SourceKeyboard      InputSource = "keyboard"
	SourceTouch         InputSource = "touch"
	SourceMenu          InputSource = "menu"
	SourceTray          InputSource = "tray"
	SourceDeepLink      InputSource = "deep-link"
	SourceAutomation    InputSource = "automation"
	SourceAccessibility InputSource = "accessibility"
)

type InputDevice string

const (
	DeviceUnknown  InputDevice = "unknown"
	DeviceMouse    InputDevice = "mouse"
	DeviceKeyboard InputDevice = "keyboard"
	DeviceTouch    InputDevice = "touch"
	DeviceSystem   InputDevice = "system"
)

var ErrInvalidMeta = errors.New("交互请求元数据无效")

type RequestMeta struct {
	RequestID      string
	CorrelationID  string
	WindowID       string
	RouteID        string
	Target         string
	Source         InputSource
	Device         InputDevice
	Modifiers      []string
	Timestamp      time.Time
	IdempotencyKey string
	PendingKey     string
}

func (meta RequestMeta) Validate() error {
	if strings.TrimSpace(meta.RequestID) == "" || strings.TrimSpace(meta.CorrelationID) == "" {
		return ErrInvalidMeta
	}
	if strings.TrimSpace(meta.WindowID) == "" || strings.TrimSpace(meta.Target) == "" {
		return ErrInvalidMeta
	}
	if !meta.Source.Valid() || !meta.Device.Valid() || meta.Timestamp.IsZero() {
		return ErrInvalidMeta
	}
	return nil
}

func (source InputSource) Valid() bool {
	switch source {
	case SourceMouse, SourceKeyboard, SourceTouch, SourceMenu, SourceTray, SourceDeepLink, SourceAutomation, SourceAccessibility:
		return true
	default:
		return false
	}
}

func (device InputDevice) Valid() bool {
	switch device {
	case DeviceUnknown, DeviceMouse, DeviceKeyboard, DeviceTouch, DeviceSystem:
		return true
	default:
		return false
	}
}

func (meta RequestMeta) EffectivePendingKey(commandType string) string {
	if strings.TrimSpace(meta.PendingKey) != "" {
		return meta.PendingKey
	}
	if strings.TrimSpace(meta.IdempotencyKey) != "" {
		return commandType + ":" + meta.IdempotencyKey
	}
	return ""
}
