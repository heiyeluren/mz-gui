// Package clipboard 提供不包含业务语义的跨平台剪贴板能力。
package clipboard

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

const DefaultMaxFiles = 32

var (
	ErrUnavailable     = errors.New("当前平台不支持读取剪贴板文件")
	ErrReadFailed      = errors.New("读取剪贴板文件失败")
	ErrInvalidFilePath = errors.New("剪贴板文件路径无效")
	ErrTooManyFiles    = errors.New("剪贴板文件数量超过上限")
)

// ReadFilePaths 读取系统剪贴板中的文件对象，并返回规范化的绝对路径。
func ReadFilePaths() ([]string, error) {
	paths, err := platformReadFilePaths()
	if err != nil {
		return nil, err
	}
	return NormalizeFilePaths(paths, DefaultMaxFiles)
}

// NormalizeFilePaths 校验、去重并保留剪贴板原始顺序。
func NormalizeFilePaths(paths []string, maxFiles int) ([]string, error) {
	if maxFiles < 1 {
		maxFiles = DefaultMaxFiles
	}
	if len(paths) > maxFiles {
		return nil, ErrTooManyFiles
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" || strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) {
			return nil, ErrInvalidFilePath
		}
		cleaned := filepath.Clean(path)
		if cleaned == "." || !filepath.IsAbs(cleaned) {
			return nil, ErrInvalidFilePath
		}
		identity := cleaned
		if runtime.GOOS == "windows" {
			identity = strings.ToLower(identity)
		}
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		result = append(result, cleaned)
	}
	return result, nil
}
