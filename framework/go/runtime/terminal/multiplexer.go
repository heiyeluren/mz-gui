package terminal

import "path/filepath"

// MultiplexerSpec 的 socket 必须在调用方已分配的私有运行目录；每个 socket 仅持有一个程序。
// ResumeOnly 只连接存活会话，缺失时返回 ErrSessionNotFound，不执行原程序。
type MultiplexerSpec struct {
	Executable string
	SocketPath string
	ResumeOnly bool
	// PageScrollFallback 为不报告鼠标事件的全屏程序把滚轮转成翻页键。
	PageScrollFallback bool
}

func validMultiplexer(s MultiplexerSpec) bool {
	return filepath.IsAbs(s.Executable) && filepath.IsAbs(s.SocketPath) &&
		len(s.Executable) <= 4096 && len(s.SocketPath) <= 100 &&
		!containsProgramControl(s.Executable) && !containsProgramControl(s.SocketPath)
}
