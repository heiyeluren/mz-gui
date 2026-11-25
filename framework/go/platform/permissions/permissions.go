// Package permissions 提供跨平台系统权限状态探测与设置入口。
package permissions

import (
	"context"
	"errors"
)

const (
	Microphone      = "microphone"
	Accessibility   = "accessibility"
	Automation      = "automation"
	ScreenRecording = "screen-recording"
	Notifications   = "notifications"
)

const (
	Authorized      = "authorized"
	NotDetermined   = "not_determined"
	Denied          = "denied"
	Restricted      = "restricted"
	ManagedBySystem = "managed_by_system"
	Unavailable     = "unavailable"
	Granted         = "granted"
	Unsupported     = "unsupported"
)

type Status struct {
	ID              string
	SystemStatus    string
	Allowed         bool
	CanOpenSettings bool
}

type SettingsOptions struct {
	ApplicationID string
}

func Snapshot(ctx context.Context) ([]Status, error) {
	if ctx == nil {
		return nil, errors.New("权限查询上下文无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return platformSnapshot(ctx)
}

func SettingsURI(permissionID string, options SettingsOptions) (string, bool) {
	return platformSettingsURI(permissionID, options)
}

func status(id, systemStatus string, allowed, canOpenSettings bool) Status {
	return Status{ID: id, SystemStatus: systemStatus, Allowed: allowed, CanOpenSettings: canOpenSettings}
}
