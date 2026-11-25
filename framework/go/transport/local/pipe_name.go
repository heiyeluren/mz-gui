package local

import (
	"crypto/sha256"
	"fmt"
)

// PipeName 计算 Windows Named Pipe 路径。
// 格式：\\.\pipe\<name>-<SID SHA-256 前 6 字节十六进制>。
func PipeName(name string, uid string) string {
	hash := sha256.Sum256([]byte(uid))
	return fmt.Sprintf(`\\.\pipe\%s-%x`, name, hash[:6])
}
