package fileopener

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidPath = errors.New("待打开文件路径无效")
var ErrInvalidURL = errors.New("待打开链接无效")

type Opener interface {
	Open(string) error
}

// URLOpener 是由上层先完成域名白名单校验后调用的系统浏览器扩展点。
type URLOpener interface {
	OpenURL(string) error
}

type Current struct{}

// OpenDirectory 只接受现有绝对目录，将目录名原样交给平台文件管理器。
func (Current) OpenDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return ErrInvalidPath
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrInvalidPath
	}
	return openDirectory(path)
}

func validExternalURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}
