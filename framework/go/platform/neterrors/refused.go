// Package neterrors 统一网络错误的跨平台语义判断。
package neterrors

import "errors"

// ConnectionRefused 只接受明确的拒绝连接错误，不把超时或权限错误当成空闲端口。
func ConnectionRefused(err error) bool {
	return errors.Is(err, connectionRefused)
}
