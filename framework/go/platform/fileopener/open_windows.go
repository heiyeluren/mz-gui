//go:build windows

package fileopener

import (
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func (Current) Open(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return ErrInvalidPath
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Run()
}

func (Current) OpenURL(value string) error {
	value = strings.TrimSpace(value)
	if !validExternalURL(value) {
		return ErrInvalidURL
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", value).Start()
}

func openDirectory(path string) error {
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	explore, _ := windows.UTF16PtrFromString("explore")
	return windows.ShellExecute(0, explore, target, nil, nil, windows.SW_SHOWNORMAL)
}
