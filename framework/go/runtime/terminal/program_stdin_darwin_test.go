//go:build darwin

package terminal

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgramStdinPreservesLongSingleLineAndDeliversEOF(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	input := []byte(strings.Repeat("长单行输入-", 12000))
	sum := fmt.Sprintf("%x", sha256.Sum256(input))
	session, err := manager.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "stdin-test", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/usr/bin/shasum", Arguments: []string{"-a", "256"}, Stdin: input}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		snapshot, err := manager.Snapshot(context.Background(), session.SessionID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Session.State == StateExited {
			var output strings.Builder
			for _, frame := range snapshot.Frames {
				output.Write(frame.Data)
			}
			if !strings.Contains(output.String(), sum) {
				t.Fatalf("stdin被PTY行缓冲截断或损坏: got=%q want=%s", output.String(), sum)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("stdin数据结束后没有真实EOF，进程仍在等待")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
