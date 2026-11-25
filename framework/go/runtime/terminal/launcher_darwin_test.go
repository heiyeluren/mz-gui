//go:build darwin

package terminal

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDarwinPTYCreateReturnsSessionQuickly(t *testing.T) {
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 1 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	session, err := manager.Create(ctx, SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "快启", Cols: 80, Rows: 24,
	})
	elapsed := time.Since(started)
	if err != nil || session.SessionID == "" {
		t.Fatalf("PTY 创建应立即返回 session id: session=%#v err=%v", session, err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("PTY 创建过慢，像在等待 shell 就绪: elapsed=%s", elapsed)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if session.CWD != canonicalRoot || session.State != StateRunning {
		t.Fatalf("终端 cwd 必须是已授权根目录: session=%#v want=%q", session, canonicalRoot)
	}
}

func TestDarwinPTYRunsUTF8InputResizeAndExit(t *testing.T) {
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 1 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	session, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "PTY 集成测试", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize(context.Background(), session.SessionID, 120, 36); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write(context.Background(), session.SessionID, []byte("printf 'MZ_UTF8_你好\\n'\nexit\n")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var snapshot Snapshot
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(context.Background(), session.SessionID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Session.State == StateExited {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snapshot.Session.State != StateExited || snapshot.Session.ExitCode == nil || *snapshot.Session.ExitCode != 0 {
		t.Fatalf("PTY 退出状态错误: %#v", snapshot.Session)
	}
	var output []byte
	for _, frame := range snapshot.Frames {
		output = append(output, frame.Data...)
	}
	if !bytes.Contains(output, []byte("MZ_UTF8_你好")) {
		t.Fatalf("PTY UTF-8 输出缺失: %q", output)
	}
}
