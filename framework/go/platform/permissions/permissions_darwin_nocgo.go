//go:build darwin && !cgo

package permissions

import (
	"context"
	"errors"
)

func platformSnapshot(_ context.Context) ([]Status, error) {
	return []Status{
		status(Microphone, Unavailable, false, true),
		status(Accessibility, Unavailable, false, true),
		status(ScreenRecording, Unavailable, false, true),
		status(Notifications, Unavailable, false, true),
	}, nil
}

func platformSystemStatus(capability string) (string, error) {
	switch capability {
	case Microphone, Accessibility, Automation, ScreenRecording, Notifications:
		return Unavailable, nil
	default:
		return "", errors.New("系统权限标识无效")
	}
}
