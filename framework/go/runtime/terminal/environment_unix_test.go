//go:build darwin || linux

package terminal

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPTYRunsComposedRuntimeEnvironment(t *testing.T) {
	root := t.TempDir()
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, root, launcher, 1)
	environment := map[string]string{"HTTP_PROXY": "", "https_proxy": "", "RUNTIME_PROBE": "composed-environment-ready"}
	for i := range 40 {
		environment[fmt.Sprintf("CONTEXT_%d", i)] = "fixture"
	}
	session, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "environment-test", Cols: 80, Rows: 24,
		Program: &ProgramSpec{Executable: "/bin/sh", Arguments: []string{"-c", `printf '%s' "$RUNTIME_PROBE"`}, Environment: environment},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := manager.Snapshot(context.Background(), session.SessionID, 0)
		if err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for _, frame := range snapshot.Frames {
			output.Write(frame.Data)
		}
		if snapshot.Session.State == StateExited && snapshot.Session.ExitCode != nil && *snapshot.Session.ExitCode == 0 && strings.Contains(output.String(), "composed-environment-ready") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("真实PTY未返回组合环境探针结果")
}
