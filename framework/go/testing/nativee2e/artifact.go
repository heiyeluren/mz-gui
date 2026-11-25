package nativee2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

type bundleInfo struct {
	Executable string `plist:"CFBundleExecutable"`
}

func ResolveArtifactExecutable(goos, artifact string) (string, error) {
	if !filepath.IsAbs(artifact) {
		return "", fmt.Errorf("%w: 制品路径必须是绝对路径", ErrInvalidConfig)
	}
	clean := filepath.Clean(artifact)
	if goos == "darwin" && strings.EqualFold(filepath.Ext(clean), ".app") {
		data, err := os.ReadFile(filepath.Join(clean, "Contents", "Info.plist"))
		if err != nil {
			return "", fmt.Errorf("读取 app bundle 信息: %w", err)
		}
		var info bundleInfo
		if _, err := plist.Unmarshal(data, &info); err != nil || !portableExecutableName(info.Executable) {
			return "", fmt.Errorf("%w: app bundle 主程序无效", ErrInvalidConfig)
		}
		return requireRegularFile(filepath.Join(clean, "Contents", "MacOS", info.Executable))
	}
	if goos == "windows" && !strings.EqualFold(filepath.Ext(clean), ".exe") {
		return "", fmt.Errorf("%w: Windows 制品必须是 EXE", ErrInvalidConfig)
	}
	return requireRegularFile(clean)
}

func portableExecutableName(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func requireRegularFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("检查制品主程序: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: 制品主程序必须是普通文件", ErrInvalidConfig)
	}
	return path, nil
}
