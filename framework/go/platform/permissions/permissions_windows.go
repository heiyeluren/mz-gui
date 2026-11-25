//go:build windows

package permissions

import (
	"context"
	"errors"
)

func platformSnapshot(_ context.Context) ([]Status, error) {
	return []Status{
		status(Microphone, ManagedBySystem, false, true),
		status(Accessibility, ManagedBySystem, false, true),
		status(ScreenRecording, ManagedBySystem, false, true),
		status(Notifications, ManagedBySystem, false, true),
	}, nil
}

func platformSystemStatus(capability string) (string, error) {
	switch capability {
	case Microphone, Accessibility, Automation, ScreenRecording, Notifications:
		return ManagedBySystem, nil
	default:
		return "", errors.New("系统权限标识无效")
	}
}

func platformSettingsURI(permissionID string, _ SettingsOptions) (string, bool) {
	values := map[string]string{
		Microphone:      "ms-settings:privacy-microphone",
		Accessibility:   "ms-settings:easeofaccess",
		Automation:      "ms-settings:privacy",
		ScreenRecording: "ms-settings:privacy-graphicscaptureprogrammatic",
		Notifications:   "ms-settings:notifications",
	}
	value, ok := values[permissionID]
	return value, ok
}
