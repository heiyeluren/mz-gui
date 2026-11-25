//go:build linux

package runtimepaths

import (
	"os"
	"strconv"
)

// Current 读取当前 Linux 用户的系统目录。
func Current(layout Layout) (Paths, error) {
	if paths, overridden, err := developmentOverride(layout); overridden {
		return paths, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	return Resolve("linux", layout, Inputs{
		HomeDir:      home,
		UserCacheDir: cache,
		TempDir:      os.TempDir(),
		UID:          strconv.Itoa(os.Getuid()),
	})
}
