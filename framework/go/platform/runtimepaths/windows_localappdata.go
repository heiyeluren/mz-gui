package runtimepaths

import (
	"errors"
	"path/filepath"
	"strings"
)

// resolveWindowsLocalAppData 把 Windows 运行根解析为 LOCALAPPDATA。
// sidecar 精简环境可能只有 USERPROFILE，此时回退到 %USERPROFILE%\AppData\Local。
func resolveWindowsLocalAppData(localAppData, userProfile string) (string, error) {
	localAppData = strings.TrimSpace(localAppData)
	if localAppData != "" {
		return localAppData, nil
	}
	userProfile = strings.TrimSpace(userProfile)
	if userProfile != "" {
		return filepath.Join(userProfile, "AppData", "Local"), nil
	}
	return "", errors.New("LOCALAPPDATA 未设置")
}
