//go:build darwin || linux

package terminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

func TestMultiplexerSurvivesManagerRestartAndExplicitCloseStopsIt(t *testing.T) {
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("需要 tmux 才能运行真实持久 PTY 验证")
	}
	root, err := os.MkdirTemp("", "mzt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, _ = filepath.EvalSymlinks(root)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var pools []*resourcepool.Pool
	makeManager := func() *Manager {
		launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{})
		pool := newTerminalResourcePool(t, 2, 2)
		pools = append(pools, pool)
		m, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 2, ResourcePool: pool})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = m.Close(context.Background()) })
		return m
	}
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "durable-test", Cols: 80, Rows: 24, Program: &ProgramSpec{Executable: "/bin/sh", Arguments: []string{"-c", "printf 'OWNER=%s\\n' \"$$\"; while IFS= read -r line; do printf 'RECEIVED:%s\\n' \"$line\"; done"}, Multiplexer: &MultiplexerSpec{Executable: tmux, SocketPath: filepath.Join(root, "terminal.sock")}}}
	waitOutput := func(m *Manager, id, want string) string {
		var output strings.Builder
		var cursor uint64
		for ctx.Err() == nil {
			s, err := m.Snapshot(ctx, id, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range s.Frames {
				output.Write(f.Data)
			}
			cursor = s.NextCursor
			if strings.Contains(output.String(), want) {
				return output.String()
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("未读到 %s", want)
		return ""
	}
	first := makeManager()
	one, err := first.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(first, one.SessionID, "OWNER=")
	pid := func() string {
		b, e := exec.CommandContext(ctx, tmux, "-S", spec.Program.Multiplexer.SocketPath, "display-message", "-p", "-t", "terminal", "#{pane_pid}").Output()
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	before := pid()
	if err := first.Write(ctx, one.SessionID, []byte("before\r")); err != nil {
		t.Fatal(err)
	}
	waitOutput(first, one.SessionID, "RECEIVED:before")
	// 生产后台先关闭任务引擎及资源池；不能因连接额度撤销而杀掉独立持久程序。
	if err := pools[0].Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if pid() != before {
		t.Fatal("后台关闭改变了原程序 PID")
	}
	second := makeManager()
	spec.Program.Multiplexer.ResumeOnly = true
	launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{})
	device, process, err := launcher.Launch(ctx, LaunchRequest{CWD: root, Executable: spec.Program.Executable, Arguments: spec.Program.Arguments, Cols: 80, Rows: 24, Multiplexer: spec.Program.Multiplexer})
	if err != nil {
		t.Fatal(err)
	}
	if err := abortTerminalLaunch(process); err != nil {
		t.Fatal(err)
	}
	_ = device.Close()
	_, _ = process.Wait()
	if pid() != before {
		t.Fatal("撤销未提交的重连终止了原程序")
	}
	two, err := second.Create(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(second, two.SessionID, "RECEIVED:before")
	if err := second.Write(ctx, two.SessionID, []byte("after\r")); err != nil {
		t.Fatal(err)
	}
	waitOutput(second, two.SessionID, "RECEIVED:after")
	if pid() != before {
		t.Fatal("重连重新执行了程序")
	}
	if err := second.CloseSession(ctx, two.SessionID, "user-stop"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Create(ctx, spec); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("显式关闭后恢复必须失败，不得重新创建: %v", err)
	}
}

func TestMultiplexerWheelUsesPageKeysForAlternateScreenWithoutMouse(t *testing.T) {
	mux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("需要 tmux")
	}
	root, err := os.MkdirTemp("", "mzw-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	root, _ = filepath.EvalSymlinks(root)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	launcher, _ := NewPlatformLauncher(PlatformLauncherOptions{})
	m, err := NewManager(ctx, ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	s, err := m.Create(ctx, SessionSpec{CWD: root, Scope: "wheel-test", ShellProfile: "system", Cols: 80, Rows: 24, Program: &ProgramSpec{
		Executable: "/bin/sh", Arguments: []string{"-c", "stty raw -echo; printf '\\033[?1049hREADY'; dd bs=1 count=8 2>/dev/null | od -An -tx1; sleep 60"},
		Multiplexer: &MultiplexerSpec{Executable: mux, SocketPath: filepath.Join(root, "t.sock"), PageScrollFallback: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseSession(context.Background(), s.SessionID, "test-complete")
	var output strings.Builder
	var cursor uint64
	sent := false
	for ctx.Err() == nil {
		snapshot, err := m.Snapshot(ctx, s.SessionID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range snapshot.Frames {
			output.Write(f.Data)
		}
		cursor = snapshot.NextCursor
		if !sent && strings.Contains(output.String(), "READY") {
			if err := m.Write(ctx, s.SessionID, []byte("\x1b[<64;10;10M\x1b[<65;10;10M")); err != nil {
				t.Fatal(err)
			}
			sent = true
		}
		if regexp.MustCompile(`1b\s+5b\s+35\s+7e\s+1b\s+5b\s+36\s+7e`).MatchString(output.String()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("滚轮未转换为 PageUp: %q", output.String())
}
