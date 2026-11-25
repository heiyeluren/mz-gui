package terminal

import "regexp"

// 仅识别完整的设备属性、RGB颜色与焦点报告；不包含光标/F键、剪贴板、换行或普通文本。
// 应答仍原样写入PTY，只是不代表用户开始输入。混合数据一律按用户输入处理。
var terminalReplyPattern = regexp.MustCompile(`^(?:\x1b\[[IO]|\x1b\[[?>][0-9]{1,5}(?:;[0-9]{1,5}){0,16}c|\x1b\](?:10|11|12);rgb:[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}/[0-9a-fA-F]{1,4}(?:\x07|\x1b\\))+$`)

func terminalReplyOnly(data []byte) bool {
	return len(data) > 0 && len(data) <= 2048 && terminalReplyPattern.Match(data)
}

// 只描述输入类别，仍递增 InputRevision；是否允许对话框继续由上层状态机决定。
var terminalNavigationPattern = regexp.MustCompile(`^(?:\r|\x1b\[[AB])+$`)

func terminalNavigationOnly(data []byte) bool {
	return len(data) > 0 && len(data) <= 64 && terminalNavigationPattern.Match(data)
}
