//go:build !windows

package neterrors

import "syscall"

// Unix 套接字直接返回系统 ECONNREFUSED。
const connectionRefused = syscall.ECONNREFUSED
