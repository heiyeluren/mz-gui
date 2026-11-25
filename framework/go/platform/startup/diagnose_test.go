package startup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiagnoseSidecarReportsSafeAndUnsafeStates(t *testing.T) {
	root := t.TempDir()
	missing := DiagnoseSidecar(filepath.Join(root, "missing-helper"), true)
	if missing.Code != SidecarMissing || missing.Unsafe() {
		t.Fatalf("缺失 sidecar 诊断错误: %#v", missing)
	}

	file := filepath.Join(root, "helper")
	if err := os.WriteFile(file, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DiagnoseSidecar(file, true); got.Code != SidecarNotExecutable {
		t.Fatalf("不可执行诊断错误: %#v", got)
	}
	if runtime.GOOS == "windows" {
		exe := filepath.Join(root, "helper.exe")
		if err := os.WriteFile(exe, []byte("ok"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := DiagnoseSidecar(exe, true); got.Code != SidecarOK || got.Unsafe() {
			t.Fatalf("Windows .exe sidecar 诊断错误: %#v", got)
		}
		file = exe
	} else {
		if err := os.Chmod(file, 0o700); err != nil {
			t.Fatal(err)
		}
		if got := DiagnoseSidecar(file, true); got.Code != SidecarOK || got.Unsafe() {
			t.Fatalf("正常 sidecar 诊断错误: %#v", got)
		}
	}

	link := filepath.Join(root, "helper-link")
	createFileLinkLike(t, file, link)
	if got := DiagnoseSidecar(link, false); got.Code != SidecarSymlink || !got.Unsafe() {
		t.Fatalf("符号链接诊断错误: %#v", got)
	}
	if got := DiagnoseSidecar("", false); got.Code != SidecarEmpty || !got.Unsafe() {
		t.Fatalf("空路径诊断错误: %#v", got)
	}
}
