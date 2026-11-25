//go:build darwin

package terminal

import (
	"context"
	"encoding/base64"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/terminal/xterm"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDarwinPTYStateSurvivesLongHiddenOutputAndRemainsInteractive(t *testing.T) {
	node, err := exec.LookPath("node")
	if value := os.Getenv("MZ_GUI_TEST_TERMINAL_NODE"); value != "" {
		node = value
		err = nil
	}
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var state *xterm.Engine
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1, CloseGrace: time.Second, ViewStateFactory: func(c, r int) (ViewState, error) { var e error; state, e = xterm.New(node, c, r); return state, e }})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	script := `import os,sys,time
os.write(1,b'\x1b[2J\x1b[1;1HPERSISTENT_HEADER')
chunk=(b'\x1b[24;1H'+b' '*70+b'\x1b[24;1Hworking')*300
for _ in range(220): os.write(1,chunk)
os.write(1,b'\x1b[12;1HREADY_AFTER_FLOOD')
line=sys.stdin.readline().strip()
os.write(1,('\x1b[14;1HACK:'+line).encode())
time.sleep(60)
`
	info, err := manager.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/usr/bin/python3", Arguments: []string{"-c", "import base64;exec(base64.b64decode('" + base64.StdEncoding.EncodeToString([]byte(script)) + "'))"}}})
	if err != nil {
		t.Fatal(err)
	}
	await := func(marker string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			s, e := manager.ViewSnapshot(context.Background(), info.SessionID, 0)
			if e == nil && strings.Contains(s.ScreenState, marker) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("真实PTY没有返回", marker)
	}
	await("READY_AFTER_FLOOD")
	raw, err := manager.Snapshot(context.Background(), info.SessionID, 0)
	if err != nil || !raw.Truncated {
		t.Fatal("没有触发真实缓存淘汰", err)
	}
	view, err := manager.ViewSnapshot(context.Background(), info.SessionID, 0)
	if err != nil || !strings.Contains(view.ScreenState, "PERSISTENT_HEADER") {
		t.Fatal("丢失当前标题", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err = manager.Write(ctx, info.SessionID, []byte("roundtrip\r"))
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	await("ACK:roundtrip")
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	err = manager.CloseSession(ctx, info.SessionID, "acceptance-stop")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Get(context.Background(), info.SessionID)
	if err != nil || result.State != StateClosed {
		t.Fatal("未取消真实PTY", err)
	}
	if state.ProcessID() != 0 {
		t.Fatal("取消后辅助进程残留")
	}
	if syscall.Kill(info.PID, 0) == nil {
		t.Fatal("取消后PTY进程残留")
	}
	t.Log("真实PTY：超过4MiB后台输出、状态恢复、命令返回、取消及进程回收均通过")
}

func TestDarwinPTYNaturalExitDrainsFinalOutput(t *testing.T) {
	node, _ := exec.LookPath("node")
	if value := os.Getenv("MZ_GUI_TEST_TERMINAL_NODE"); value != "" {
		node = value
	}
	launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{TermProgram: "framework-test"})
	root := t.TempDir()
	m, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1, ViewStateFactory: func(c, r int) (ViewState, error) {
		state, err := xterm.New(node, c, r)
		return slowExitState{state}, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	info, err := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/usr/bin/python3", Arguments: []string{"-c", "import os;os.write(1,b'X'*(16<<10)+b'FINAL_OUTPUT_MUST_SURVIVE')"}}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, e := m.ViewSnapshot(context.Background(), info.SessionID, 0)
		if e != nil {
			t.Fatal(e)
		}
		if terminalState(s.Session.State) {
			if !strings.Contains(s.ScreenState, "FINAL_OUTPUT_MUST_SURVIVE") {
				t.Fatal("自然退出丢失最后输出")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("自然退出没有完成收尾")
}

type slowExitState struct{ *xterm.Engine }

func (s slowExitState) Write(data []byte) error {
	time.Sleep(50 * time.Millisecond)
	return s.Engine.Write(data)
}
