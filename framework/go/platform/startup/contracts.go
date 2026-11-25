// Package startup 提供产品无关的 sidecar 启动与登录注册契约。
package startup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	ErrInvalidSidecarName = errors.New("sidecar 名称无效")
	ErrUnsafeSidecar      = errors.New("sidecar 必须是普通文件且不能是符号链接")
	ErrUnsupportedOS      = errors.New("sidecar 平台不受支持")
)

var sidecarNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type RegistrationStatus struct {
	Enabled    bool   `json:"enabled"`
	Drifted    bool   `json:"drifted"`
	Strategy   string `json:"strategy"`
	Location   string `json:"location"`
	Executable string `json:"executable"`
}

type Registration interface {
	SetEnabled(context.Context, bool) error
	Status(context.Context) (RegistrationStatus, error)
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

func ResolveSiblingExecutable(desktopExecutable, sidecarName, goos string) (string, error) {
	if !sidecarNamePattern.MatchString(sidecarName) || filepath.Base(sidecarName) != sidecarName {
		return "", ErrInvalidSidecarName
	}
	if !filepath.IsAbs(desktopExecutable) {
		return "", errors.New("桌面进程路径必须是绝对路径")
	}
	name := sidecarName
	switch goos {
	case "windows":
		name += ".exe"
	case "darwin", "linux":
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedOS, goos)
	}
	var lastErr error
	for _, candidate := range sidecarCandidatePaths(desktopExecutable, name) {
		path, err := acceptSidecarExecutable(candidate, goos)
		if err == nil {
			return path, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("读取 sidecar")
	}
	return "", lastErr
}

func sidecarCandidatePaths(desktopExecutable, name string) []string {
	desktop := filepath.Clean(desktopExecutable)
	seen := map[string]struct{}{}
	var paths []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		candidate := filepath.Join(dir, name)
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		paths = append(paths, candidate)
	}
	if bundle := bundleMacOSDir(desktop); bundle != "" {
		add(bundle)
		return paths
	}
	if info, err := os.Lstat(desktop); err == nil && isLinkLike(info) {
		if resolved, err := filepath.EvalSymlinks(desktop); err == nil && resolved != "" {
			resolved = filepath.Clean(resolved)
			if bundle := bundleMacOSDir(resolved); bundle != "" {
				add(bundle)
				return paths
			}
			add(filepath.Dir(resolved))
		}
	}
	add(filepath.Dir(desktop))
	return paths
}

func bundleMacOSDir(desktop string) string {
	dir := filepath.Dir(filepath.Clean(desktop))
	if filepath.Base(dir) != "MacOS" {
		return ""
	}
	contents := filepath.Dir(dir)
	if filepath.Base(contents) != "Contents" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(filepath.Dir(contents)), ".app") {
		return ""
	}
	return dir
}

func acceptSidecarExecutable(path, goos string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("读取 sidecar: %w", err)
	}
	if isLinkLike(info) || !info.Mode().IsRegular() {
		return "", ErrUnsafeSidecar
	}
	if runtime.GOOS == goos && !fileLooksExecutable(path, info.Mode()) {
		return "", errors.New("sidecar 不可执行")
	}
	return path, nil
}
