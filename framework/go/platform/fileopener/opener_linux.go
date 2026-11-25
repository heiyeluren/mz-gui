//go:build linux

package fileopener

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func (Current) Open(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return ErrInvalidPath
	}
	return exec.Command("xdg-open", path).Run()
}

func (Current) OpenURL(value string) error {
	value = strings.TrimSpace(value)
	if !validExternalURL(value) {
		return ErrInvalidURL
	}
	return exec.Command("xdg-open", value).Run()
}

func openDirectory(path string) error {
	return exec.Command("xdg-open", path).Run()
}
