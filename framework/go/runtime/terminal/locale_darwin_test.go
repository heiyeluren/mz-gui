//go:build darwin

package terminal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDarwinTerminalLocaleDefaultsToUTF8(t *testing.T) {
	for _, name := range []string{"LANG", "LC_CTYPE", "LC_ALL"} {
		t.Setenv(name, "")
	}
	output := runLocalePTY(t, "/usr/bin/locale charmap", nil)
	if strings.TrimSpace(output) != "UTF-8" {
		t.Fatalf("GUI 无 locale 时终端必须使用 UTF-8，实际 %q", output)
	}
	for _, name := range []string{"LANG", "LC_CTYPE", "LC_ALL"} {
		if os.Getenv(name) != "" {
			t.Fatalf("不得修改父进程 %s", name)
		}
	}
}

// 只在显式启用时使用原本为空的 Ruler 剪贴板；绝不写普通剪贴板。
func TestDarwinTerminalClipboardUsesUTF8(t *testing.T) {
	if os.Getenv("MOZI_TEST_TERMINAL_CLIPBOARD") != "1" {
		t.Skip("显式启用 macOS 独立剪贴板回归")
	}
	const board = `ObjC.import("AppKit"); var p=$.NSPasteboard.pasteboardWithName($.NSPasteboardNameRuler); `
	data, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", board+`JSON.stringify(Number(p.types.count));`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "0" {
		t.Skip("Ruler 剪贴板已有内容，不覆盖用户数据")
	}
	owned := ""
	t.Cleanup(func() {
		encoded, _ := json.Marshal(owned)
		_, err := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", board+`if(ObjC.unwrap(p.stringForType($.NSPasteboardTypeString)) === `+string(encoded)+`) { p.clearContents; }`).CombinedOutput()
		if err != nil {
			t.Errorf("清理测试剪贴板失败: %v", err)
		}
	})
	for _, name := range []string{"LANG", "LC_CTYPE", "LC_ALL"} {
		t.Setenv(name, "")
	}
	const sample = "终端复制测试：中文 ABC 😀"
	output := runLocalePTY(t, "/usr/bin/pbcopy -pboard ruler && LC_CTYPE=UTF-8 /usr/bin/pbpaste -pboard ruler", []byte(sample))
	owned = output
	if output != sample {
		t.Fatalf("真实终端复制损坏: got=%q want=%q", output, sample)
	}
}

func runLocalePTY(t *testing.T, command string, input []byte) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-locale-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ctx, ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	info, err := manager.Create(ctx, SessionSpec{CWD: root, ShellProfile: "system", Scope: "locale-test", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/bin/sh", Arguments: []string{"-c", command + "; test_exit=$?; sleep 0.05; exit $test_exit"}, Stdin: input}})
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.session(info.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = waitReaderDone(ctx, session); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(ctx, info.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Session.ExitCode == nil || *snapshot.Session.ExitCode != 0 {
		t.Fatalf("子进程退出异常: %+v", snapshot.Session)
	}
	var output strings.Builder
	for _, frame := range snapshot.Frames {
		output.Write(frame.Data)
	}
	return output.String()
}
