//go:build darwin

package terminal

// SupportsProgramStdin 表示当前平台可在保留PTY输出的同时给子进程独立stdin/EOF。
func SupportsProgramStdin() bool { return true }
