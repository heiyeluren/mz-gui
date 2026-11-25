//go:build windows

package neterrors

import "golang.org/x/sys/windows"

// Windows 套接字使用 Winsock 错误码，不能用 syscall 中合成的 POSIX 值。
const connectionRefused = windows.WSAECONNREFUSED
