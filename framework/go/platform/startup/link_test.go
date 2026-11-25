package startup

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func createFileLinkLike(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("创建符号链接: %v", err)
	} else {
		t.Skipf("当前 Windows 无法创建文件符号链接: %v", err)
	}
}

func createDirLinkLike(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatalf("创建符号链接: %v", err)
	} else {
		symlinkErr := err
		out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if jerr == nil {
			return
		}
		t.Skipf("当前 Windows 无法创建符号链接或 junction: symlink=%v junction=%v %s", symlinkErr, jerr, out)
	}
}
