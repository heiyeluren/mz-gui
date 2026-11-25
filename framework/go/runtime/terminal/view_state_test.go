package terminal

import (
	"context"
	"errors"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/terminal/xterm"
	"os/exec"
	"strings"
	"testing"
)

func viewManager(t *testing.T, max int) (*Manager, *fakeLauncher, string) {
	t.Helper()
	root := t.TempDir()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	l := &fakeLauncher{}
	m, err := NewManager(context.Background(), ManagerOptions{Launcher: l, AllowedRoots: []string{root}, MaxSessions: max, BufferBytes: 4 << 20, ViewStateFactory: func(c, r int) (ViewState, error) { return xterm.New(node, c, r) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m, l, root
}
func TestManagedScreenRepliesWhileViewHiddenWithoutClaimingUserInput(t *testing.T) {
	m, l, root := viewManager(t, 1)
	info, err := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	l.last().device.emit([]byte("\x1b[6n\x1b]11;?\x07"))
	waitForTerminal(t, func() bool { return strings.Contains(string(l.last().device.written()), "rgb:1818/1c1c/2020") })
	got, _ := m.Get(context.Background(), info.SessionID)
	if got.InputRevision != 0 {
		t.Fatal("设备应答冒充用户输入")
	}
	s, err := m.ViewSnapshot(context.Background(), info.SessionID, 0)
	if err != nil || s.ScreenState == "" || !s.ScreenManaged {
		t.Fatal("没有画面状态", err)
	}
	before := string(l.last().device.written())
	m.ViewSnapshot(context.Background(), info.SessionID, 0)
	if string(l.last().device.written()) != before {
		t.Fatal("重复恢复触发伪输入")
	}
	if err := m.Write(context.Background(), info.SessionID, []byte("用户输入\r")); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(context.Background(), info.SessionID)
	if got.InputRevision != 1 {
		t.Fatal("真实输入版本没有增长")
	}
}
func TestEightManagedScreensRemainIsolated(t *testing.T) {
	m, l, root := viewManager(t, 8)
	for i := 0; i < 8; i++ {
		info, err := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Repeat(string(rune('A'+i)), 10)
		l.last().device.emit([]byte(text))
		waitForTerminal(t, func() bool {
			s, e := m.ViewSnapshot(context.Background(), info.SessionID, 0)
			return e == nil && strings.Contains(s.ScreenState, text)
		})
		if err := m.Resize(context.Background(), info.SessionID, 100, 30); err != nil {
			t.Fatal(err)
		}
		s, e := m.ViewSnapshot(context.Background(), info.SessionID, 0)
		if e != nil || !strings.Contains(s.ScreenState, `"cols":100`) {
			t.Fatal(e, s.ScreenState)
		}
	}
}
func TestStateFailureNeverKillsPTY(t *testing.T) {
	root := t.TempDir()
	l := &fakeLauncher{}
	state := &failedState{}
	m, e := NewManager(context.Background(), ManagerOptions{Launcher: l, AllowedRoots: []string{root}, MaxSessions: 1, ViewStateFactory: func(int, int) (ViewState, error) { return state, nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close(context.Background())
	info, e := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24})
	if e != nil {
		t.Fatal(e)
	}
	l.last().device.emit([]byte("输出"))
	waitForTerminal(t, func() bool { s, _ := m.Snapshot(context.Background(), info.SessionID, 0); return len(s.Frames) > 0 })
	if _, e = m.ViewSnapshot(context.Background(), info.SessionID, 0); !errors.Is(e, ErrViewStateUnavailable) {
		t.Fatal(e)
	}
	if e = m.Write(context.Background(), info.SessionID, []byte("继续\r")); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(l.last().device.written()), "继续") {
		t.Fatal("状态故障中断输入")
	}
	got, _ := m.Get(context.Background(), info.SessionID)
	if got.State != StateRunning {
		t.Fatal("状态故障改变业务生命周期")
	}
}

type failedState struct{}

func (*failedState) Write([]byte) error         { return errors.New("fixture state fault") }
func (*failedState) Resize(int, int) error      { return nil }
func (*failedState) Serialize() (string, error) { return "", errors.New("fixture") }
func (*failedState) Close()                     {}
func TestOutputEndWaitsForReader(t *testing.T) {
	s := &managedSession{info: SessionInfo{State: StateExited}, readerDone: make(chan struct{})}
	if s.outputInfo().State != StateClosing {
		t.Fatal("尾部未读完就返回终态")
	}
	close(s.readerDone)
	if s.outputInfo().State != StateExited {
		t.Fatal("读尽后没有恢复真实终态")
	}
}

// 浅色会话在界面尚未打开时也必须回答真实背景，且不影响默认深色会话。
func TestSessionDefaultColorsBeforeFirstOutput(t *testing.T) {
	m, l, root := viewManager(t, 2)
	for _, tc := range []struct {
		colors DefaultColors
		want   string
	}{
		{DefaultColors{Foreground: "#252721", Background: "#e6e2d9", Cursor: "#4e356e"}, "rgb:e6e6/e2e2/d9d9"},
		{DefaultColors{}, "rgb:1818/1c1c/2020"},
	} {
		info, err := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24, DefaultColors: tc.colors})
		if err != nil {
			t.Fatal(err)
		}
		device := l.last().device
		device.emit([]byte("\x1b]11;?\x07"))
		waitForTerminal(t, func() bool { return strings.Contains(string(device.written()), tc.want) })
		got, _ := m.Get(context.Background(), info.SessionID)
		if got.InputRevision != 0 {
			t.Fatal("颜色查询不能计作用户输入")
		}
	}
}

// 无效颜色在创建 PTY 前拒绝，不消耗会话名额。
func TestInvalidDefaultColorsDoNotLaunch(t *testing.T) {
	m, l, root := viewManager(t, 1)
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}
	for _, color := range []string{"red", "#12345g", "#ffffff\x1b]11;?\x07"} {
		spec.DefaultColors.Background = color
		if _, err := m.Create(context.Background(), spec); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("应拒绝非法颜色：%v", err)
		}
	}
	if l.count() != 0 {
		t.Fatal("非法颜色触发了程序启动")
	}
	spec.DefaultColors.Background = "#e6e2d9"
	if _, err := m.Create(context.Background(), spec); err != nil {
		t.Fatal("无效请求占用了会话名额", err)
	}
}
