//go:build darwin

package fileopener

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func (Current) Open(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return ErrInvalidPath
	}
	return exec.Command("/usr/bin/open", path).Run()
}

func (Current) OpenURL(value string) error {
	value = strings.TrimSpace(value)
	if !validExternalURL(value) {
		return ErrInvalidURL
	}
	return exec.Command("/usr/bin/open", value).Start()
}

// 指定访达的浏览根目录，即使目录本身是应用包，也只浏览其内容。
// 路径通过独立参数传入；固定脚本仅调用 AppKit，无需访达自动化授权。
func openDirectory(path string) error {
	const browse = `ObjC.import("AppKit");
function run(argv) {
	if (!$.NSWorkspace.sharedWorkspace.selectFileInFileViewerRootedAtPath($(), argv[0])) {
		throw new Error("访达未能打开目录");
	}
}`
	return exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", browse, path).Run()
}
