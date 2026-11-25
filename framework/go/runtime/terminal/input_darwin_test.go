//go:build darwin

package terminal

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinPTYInputRevisionPreservesUnEchoedUserInput(t *testing.T) {
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1, CloseGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	created, err := manager.Create(ctx, SessionSpec{CWD: root, ShellProfile: "system", Scope: "input-test", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/bin/sh", Arguments: []string{"-c", "stty -echo; printf 'INPUT_READY\\n'; IFS= read -r line; printf 'VALUE=%s\\n' \"$line\""}}})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	var cursor uint64
	read := func() SessionInfo {
		t.Helper()
		snapshot, err := manager.Snapshot(ctx, created.SessionID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		cursor = snapshot.NextCursor
		for _, frame := range snapshot.Frames {
			output.Write(frame.Data)
		}
		return snapshot.Session
	}
	waitForTerminal(t, func() bool { read(); return strings.Contains(output.String(), "INPUT_READY") })
	if err := manager.Write(ctx, created.SessionID, []byte("用户原文")); err != nil {
		t.Fatal(err)
	}
	if got, err := manager.WriteIfInputUnchanged(ctx, created.SessionID, created.InputRevision, []byte("AUTO_SHOULD_NOT_APPEAR\r")); !errors.Is(err, ErrInputChanged) || got != 1 {
		t.Fatalf("真实PTY未拒绝过期输入版本: revision=%d err=%v", got, err)
	}
	// stty -echo 下用户输入仍在内核终端行缓冲内，只有显式新动作提交它。
	if got, err := manager.WriteIfInputUnchanged(ctx, created.SessionID, 1, []byte("\r")); err != nil || got != 2 {
		t.Fatalf("明确的新输入提交失败: revision=%d err=%v", got, err)
	}
	var terminalInfo SessionInfo
	waitForTerminal(t, func() bool { terminalInfo = read(); return terminalInfo.State == StateExited })
	read()
	if terminalInfo.ExitCode == nil || *terminalInfo.ExitCode != 0 || !strings.Contains(output.String(), "VALUE=用户原文") || strings.Contains(output.String(), "AUTO_SHOULD_NOT_APPEAR") {
		t.Fatalf("真实读取值或退出状态不符: %#v output=%q", terminalInfo, output.String())
	}
	if err := syscall.Kill(created.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("真实子进程未释放: pid=%d err=%v", created.PID, err)
	}
}
