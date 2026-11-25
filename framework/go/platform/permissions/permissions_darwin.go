//go:build darwin

package permissions

import (
	"net/url"
	"regexp"
)

var applicationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,254}$`)

func platformSettingsURI(permissionID string, options SettingsOptions) (string, bool) {
	values := map[string]string{
		Microphone:      "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone",
		Accessibility:   "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
		Automation:      "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation",
		ScreenRecording: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
	}
	if value, ok := values[permissionID]; ok {
		return value, true
	}
	if permissionID == Notifications {
		base := "x-apple.systempreferences:com.apple.Notifications-Settings.extension"
		if applicationIDPattern.MatchString(options.ApplicationID) {
			return base + "?id=" + url.QueryEscape(options.ApplicationID), true
		}
		return base, true
	}
	return "", false
}

func statusFromNative(id string, code int) Status {
	systemStatus, allowed := Unavailable, false
	switch code {
	case 0:
		systemStatus, allowed = Authorized, true
	case 1:
		systemStatus = NotDetermined
	case 2:
		systemStatus = Denied
	case 3:
		systemStatus = Restricted
	}
	return status(id, systemStatus, allowed, true)
}
