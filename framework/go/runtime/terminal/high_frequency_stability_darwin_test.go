//go:build darwin

package terminal

import (
	"context"
	"testing"
	"time"
)

func TestDarwinPTYHighFrequencyOutputStaysBoundedAndWritable(t *testing.T) {
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 4 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	session, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "PTY 高频输出", Cols: 80, Rows: 24,
		Program: &ProgramSpec{
			Executable: "/usr/bin/python3",
			Arguments:  []string{"-c", "import sys,time; sys.stdout.buffer.write(b'X'*(12<<20)); sys.stdout.flush(); time.sleep(8)"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var snapshot Snapshot
	for {
		used := sessionRingUsed(manager, session.SessionID)
		current, snapshotErr := manager.Snapshot(context.Background(), session.SessionID, 0)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		snapshot = current
		if snapshot.Truncated && used > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 PTY 高频输出超时 used=%d snapshot=%#v", used, snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if used := sessionRingUsed(manager, session.SessionID); used > 4<<20 {
		t.Fatalf("真实 PTY 高频输出后 ring 越界: used=%d", used)
	}

	var cursor, last uint64
	for i := 0; i < 20; i++ {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), session.SessionID, cursor)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if snapshot.NextCursor < last {
			t.Fatalf("PTY 快照游标回退: last=%d next=%d", last, snapshot.NextCursor)
		}
		last = snapshot.NextCursor
		cursor = snapshot.NextCursor
	}

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelWrite()
	if err := manager.Write(writeCtx, session.SessionID, []byte("\x03")); err != nil {
		t.Fatalf("PTY 高压下输入不可写: %v", err)
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClose()
	if err := manager.CloseSession(closeCtx, session.SessionID, "pty-high-frequency-stop"); err != nil {
		t.Fatalf("关闭真实 PTY 失败: %v", err)
	}
	info, err := manager.Get(context.Background(), session.SessionID)
	if err != nil || !terminalState(info.State) {
		t.Fatalf("PTY 关闭后应变为终态: %#v err=%v", info, err)
	}
	if cursor == 0 && last == 0 {
		t.Fatal("PTY 高频输出后游标应前进")
	}
}
