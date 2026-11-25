package terminal

import "encoding/hex"

// DefaultColors 定义终端设备的默认颜色；空值沿用原主题，非空值限定为 #RRGGBB。
// 仅作用于当前会话，CLI 的 OSC 调色板覆盖及重置继续遵守终端协议。
type DefaultColors struct {
	Foreground string
	Background string
	Cursor     string
}

func (c DefaultColors) valid() bool {
	for _, value := range []string{c.Foreground, c.Background, c.Cursor} {
		if value == "" {
			continue
		}
		if len(value) != 7 || value[0] != '#' {
			return false
		}
		if _, err := hex.DecodeString(value[1:]); err != nil {
			return false
		}
	}
	return true
}
