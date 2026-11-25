//go:build darwin && cgo

package permissions

/*
#cgo CFLAGS: -fblocks
#cgo LDFLAGS: -framework ApplicationServices -framework AVFoundation -framework CoreGraphics -framework Foundation -framework UserNotifications
int gui_permission_microphone_status(void);
int gui_permission_accessibility_status(void);
int gui_permission_screen_recording_status(void);
int gui_permission_notifications_status(void);
int gui_permission_automation_status(void);
*/
import "C"

import (
	"context"
	"errors"
)

func platformSnapshot(_ context.Context) ([]Status, error) {
	return []Status{
		statusFromNative(Microphone, int(C.gui_permission_microphone_status())),
		statusFromNative(Accessibility, int(C.gui_permission_accessibility_status())),
		statusFromNative(ScreenRecording, int(C.gui_permission_screen_recording_status())),
		statusFromNative(Notifications, int(C.gui_permission_notifications_status())),
	}, nil
}

func platformSystemStatus(capability string) (string, error) {
	var code int
	switch capability {
	case Microphone:
		code = int(C.gui_permission_microphone_status())
	case Accessibility:
		code = int(C.gui_permission_accessibility_status())
	case ScreenRecording:
		code = int(C.gui_permission_screen_recording_status())
	case Automation:
		code = int(C.gui_permission_automation_status())
	case Notifications:
		code = int(C.gui_permission_notifications_status())
	default:
		return "", errors.New("系统权限标识无效")
	}
	return statusFromNative(capability, code).SystemStatus, nil
}
