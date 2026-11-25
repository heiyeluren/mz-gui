package local

import (
	"fmt"
	"path/filepath"
)

const maxEndpointNameBytes = 64

// Endpoint 描述一个与产品无关的本机 IPC 端点。
type Endpoint struct {
	Name   string
	RunDir string
}

// Validate 保证端点名称可安全用于 UDS 文件名和 Windows Named Pipe。
func (endpoint Endpoint) Validate() error {
	if endpoint.Name == "" {
		return fmt.Errorf("本机 IPC 端点名称不能为空")
	}
	if len([]byte(endpoint.Name)) > maxEndpointNameBytes {
		return fmt.Errorf("本机 IPC 端点名称不能超过 %d 字节", maxEndpointNameBytes)
	}
	for index, character := range []byte(endpoint.Name) {
		alphaNumeric := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if index == 0 && !alphaNumeric {
			return fmt.Errorf("本机 IPC 端点名称必须以 ASCII 字母或数字开头")
		}
		if !alphaNumeric && character != '-' && character != '_' && character != '.' {
			return fmt.Errorf("本机 IPC 端点名称只能包含 ASCII 字母、数字、点、下划线和连字符")
		}
	}
	if endpoint.Name[len(endpoint.Name)-1] == '.' {
		return fmt.Errorf("本机 IPC 端点名称不能以点结尾")
	}
	if endpoint.RunDir == "" {
		return fmt.Errorf("本机 IPC 运行目录不能为空")
	}
	if !filepath.IsAbs(endpoint.RunDir) {
		return fmt.Errorf("本机 IPC 运行目录必须是绝对路径")
	}
	return nil
}

// Target 返回仅用于 gRPC 解析器标识的逻辑地址。
func (endpoint Endpoint) Target() string {
	return "passthrough:///" + endpoint.Name
}
