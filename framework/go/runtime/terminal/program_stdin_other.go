//go:build !darwin

package terminal

// SupportsProgramStdin 为false时调用方保留已有PTY输入通道，不伪报独立stdin已支持。
func SupportsProgramStdin() bool { return false }
