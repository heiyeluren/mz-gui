//go:build windows

package runtimepaths

import (
	"os"
)

// Current 读取当前 Windows 用户的 LOCALAPPDATA。
func Current(layout Layout) (Paths, error) {
	if paths, overridden, err := developmentOverride(layout); overridden {
		return paths, err
	}
	localAppData, err := resolveWindowsLocalAppData(os.Getenv("LOCALAPPDATA"), os.Getenv("USERPROFILE"))
	if err != nil {
		return Paths{}, err
	}
	return Resolve("windows", layout, Inputs{LocalAppData: localAppData})
}
