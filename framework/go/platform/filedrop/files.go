// Package filedrop 提供不包含业务授权语义的系统文件拖放路径校验。
package filedrop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	ErrInvalidFile  = errors.New("拖放文件无效")
	ErrNetworkFile  = errors.New("拖放网络文件不受支持")
	ErrTooManyFiles = errors.New("拖放文件数量超过上限")
)

// NormalizeRegularFiles 校验、去重并保留系统原生拖放顺序。
func NormalizeRegularFiles(paths []string, maxFiles int) ([]string, error) {
	if maxFiles < 1 || len(paths) > maxFiles {
		return nil, ErrTooManyFiles
	}
	values := make([]string, 0, len(paths))
	identities := make([]os.FileInfo, 0, len(paths))
	for _, value := range paths {
		if value == "" || strings.ContainsRune(value, '\x00') || !filepath.IsAbs(value) {
			return nil, ErrInvalidFile
		}
		path := filepath.Clean(value)
		if runtime.GOOS == "windows" && (strings.HasPrefix(path, `\\`) || strings.HasPrefix(filepath.ToSlash(path), "//")) {
			return nil, ErrNetworkFile
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalidFile
		}
		duplicate := false
		for _, identity := range identities {
			if os.SameFile(identity, info) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		identities = append(identities, info)
		values = append(values, path)
	}
	return values, nil
}
