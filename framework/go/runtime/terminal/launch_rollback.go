package terminal

// 重连未提交或超时时只能撤销新连接，不能终止重连前已经存活的程序。
func abortTerminalLaunch(process Process) error {
	if owner, ok := process.(interface{ AbortLaunch() error }); ok {
		return owner.AbortLaunch()
	}
	return process.KillTree()
}
