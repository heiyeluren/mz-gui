//go:build !darwin && !linux && !windows

package diskspace

import "errors"

func availableBytes(string) (uint64, error) {
	return 0, errors.New("当前平台不支持磁盘空间查询")
}
