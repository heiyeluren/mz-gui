package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

func TestManagerSessionLifecycleAndOutputCursor(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 2)

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "主终端", Cols: 100, Rows: 24,
	})
	canonicalRoot, canonicalErr := filepath.EvalSymlinks(root)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if err != nil || created.State != StateRunning || created.CWD != canonicalRoot || created.PID != 42 {
		t.Fatalf("创建终端会话失败: session=%#v err=%v", created, err)
	}
	running := launcher.last()
	running.device.emit([]byte("hello 世界\r\n"))
	waitForTerminal(t, func() bool {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), created.SessionID, 0)
		return snapshotErr == nil && len(snapshot.Frames) == 1
	})

	snapshot, err := manager.Snapshot(context.Background(), created.SessionID, 0)
	if err != nil || snapshot.Session.SessionID != created.SessionID || string(snapshot.Frames[0].Data) != "hello 世界\r\n" {
		t.Fatalf("终端快照错误: snapshot=%#v err=%v", snapshot, err)
	}
	if err := manager.Write(context.Background(), created.SessionID, []byte("pwd\r")); err != nil {
		t.Fatalf("写入终端失败: %v", err)
	}
	if got := string(running.device.written()); got != "pwd\r" {
		t.Fatalf("终端输入 = %q", got)
	}
	if err := manager.Resize(context.Background(), created.SessionID, 120, 40); err != nil {
		t.Fatalf("终端 resize 失败: %v", err)
	}
	if running.device.cols != 120 || running.device.rows != 40 {
		t.Fatalf("终端尺寸 = %dx%d", running.device.cols, running.device.rows)
	}
	if err := manager.CloseSession(context.Background(), created.SessionID, "user-close"); err != nil {
		t.Fatalf("关闭终端失败: %v", err)
	}
	closed, err := manager.Get(context.Background(), created.SessionID)
	if err != nil || closed.State != StateClosed || closed.ExitCode == nil || *closed.ExitCode != 0 || running.process.interrupts != 1 {
		t.Fatalf("终端关闭状态错误: session=%#v process=%#v err=%v", closed, running.process, err)
	}
}

func TestManagerPreservesUTF8AcrossDeviceReadBoundaries(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)
	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 100, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	running := launcher.last()
	running.device.emit([]byte{0xe4, 0xb8})
	time.Sleep(20 * time.Millisecond)

	partial, err := manager.Snapshot(context.Background(), created.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Frames) != 0 {
		t.Fatalf("不完整 UTF-8 不得提前形成输出帧: %#v", partial.Frames)
	}

	running.device.emit([]byte{0xad, 'A'})
	waitForTerminal(t, func() bool {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), created.SessionID, 0)
		return snapshotErr == nil && len(snapshot.Frames) == 1
	})
	snapshot, err := manager.Snapshot(context.Background(), created.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(snapshot.Frames[0].Data); got != "中A" {
		t.Fatalf("跨帧 UTF-8 输出 = %q", got)
	}
}

func TestManagerRejectsUngradedPathsAndInvalidRequests(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	manager := newTestManager(t, root, &fakeLauncher{}, 1)

	cases := []SessionSpec{
		{CWD: outside, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "custom", Scope: "workspace-terminal", Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "bad scope", Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 10, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 300},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Program: &ProgramSpec{Executable: "relative-program"}, Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Program: &ProgramSpec{Executable: "/usr/bin/env", Arguments: []string{"ok\ninvalid"}}, Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Program: &ProgramSpec{Executable: "/usr/bin/env", Arguments: []string{"ok\tinvalid"}}, Cols: 80, Rows: 24},
		{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Program: &ProgramSpec{Executable: "/usr/bin/env", Environment: map[string]string{"bad-key": "value"}}, Cols: 80, Rows: 24},
	}
	for index, spec := range cases {
		if _, err := manager.Create(context.Background(), spec); !errors.Is(err, ErrInvalidArgument) && !errors.Is(err, ErrPathNotGranted) {
			t.Fatalf("非法终端请求 #%d 错误 = %v", index, err)
		}
	}
	if err := manager.Write(context.Background(), "missing", []byte("x")); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("未知会话写入错误 = %v", err)
	}
	if err := manager.Write(context.Background(), "missing", make([]byte, MaxInputBytes+1)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("超大输入错误 = %v", err)
	}
}

func TestManagerAllowsPrevalidatedWorkspaceCWDOutsideStaticRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: outside, ShellProfile: "system", Scope: "kongming-loop", Title: "已授权工作区",
		Program: &ProgramSpec{Executable: "/usr/bin/env"}, PrevalidatedCWD: true, Cols: 80, Rows: 24,
	})
	canonicalOutside, canonicalErr := filepath.EvalSymlinks(outside)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if err != nil || created.CWD != canonicalOutside || launcher.last().request.CWD != canonicalOutside {
		t.Fatalf("已预授权工作目录应允许启动: session=%#v request=%#v err=%v", created, launcher.last().request, err)
	}
}

func TestManagerPassesControlledProgramWithoutShell(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)
	_, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "agent-runtime", Title: "Codex",
		Program: &ProgramSpec{Executable: "/usr/bin/codex", Arguments: []string{"-m", "gpt-test"}, Environment: map[string]string{"PYTHONUNBUFFERED": "1"}}, Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := launcher.last().request
	if request.Executable != "/usr/bin/codex" || len(request.Arguments) != 2 || request.Arguments[1] != "gpt-test" {
		t.Fatalf("受控程序未原样传给平台适配器: %#v", request)
	}
	if request.Environment["PYTHONUNBUFFERED"] != "1" {
		t.Fatalf("受控程序环境未传给平台适配器: %#v", request.Environment)
	}
}

func TestManagerDetachAndAttachPreservesRunningProcess(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "可重连终端", Cols: 100, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	running := launcher.last()
	running.device.emit([]byte("before-detach\n"))
	waitForTerminal(t, func() bool {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), created.SessionID, 0)
		return snapshotErr == nil && snapshot.NextCursor > 0
	})
	beforeDetach, err := manager.Snapshot(context.Background(), created.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}

	detached, err := manager.Detach(context.Background(), created.SessionID, "window-hidden")
	if err != nil {
		t.Fatal(err)
	}
	if detached.State != StateDetached || detached.PID != created.PID {
		t.Fatalf("detach 后应保留进程并进入 detached: %#v", detached)
	}
	running.process.mu.Lock()
	interrupts, kills := running.process.interrupts, running.process.kills
	running.process.mu.Unlock()
	if interrupts != 0 || kills != 0 {
		t.Fatalf("detach 不能终止终端进程: interrupts=%d kills=%d", interrupts, kills)
	}
	running.device.emit([]byte("while-detached\n"))
	waitForTerminal(t, func() bool {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), created.SessionID, beforeDetach.NextCursor)
		return snapshotErr == nil && len(snapshot.Frames) == 1
	})

	attached, err := manager.Attach(context.Background(), created.SessionID, beforeDetach.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if attached.Session.State != StateRunning || attached.Session.PID != created.PID || len(attached.Frames) != 1 || string(attached.Frames[0].Data) != "while-detached\n" {
		t.Fatalf("attach 应恢复 running 并返回断线后输出: %#v", attached)
	}
}

func TestManagerCreateReturnsSessionIDImmediatelyAndHonorsDeadline(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	created, err := manager.Create(ctx, SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "快启", Cols: 80, Rows: 24,
	})
	cancel()
	if err != nil || created.SessionID == "" {
		t.Fatalf("创建终端应立即返回 session id: session=%#v err=%v", created, err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatalf("终端创建不得卡住等待输出: elapsed=%s", time.Since(started))
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if created.CWD != canonicalRoot || created.State != StateRunning {
		t.Fatalf("终端 cwd 必须诚实可见: session=%#v want=%q", created, canonicalRoot)
	}

	blocking := &blockingLauncher{started: make(chan struct{})}
	slow := newTestManager(t, root, blocking, 1)
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer deadlineCancel()
	done := make(chan error, 1)
	go func() {
		_, createErr := slow.Create(deadlineCtx, SessionSpec{
			CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "超时", Cols: 80, Rows: 24,
		})
		done <- createErr
	}()
	select {
	case <-blocking.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("阻塞启动器未被调用")
	}
	select {
	case createErr := <-done:
		if !errors.Is(createErr, context.DeadlineExceeded) && !errors.Is(createErr, context.Canceled) {
			t.Fatalf("终端创建超时错误 = %v", createErr)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("终端创建超过 caller deadline 后仍未返回")
	}
}

func TestManagerDefaultsEmptyWorkingDirectoryToFirstGrantedRoot(t *testing.T) {
	root := t.TempDir()
	manager := newTestManager(t, root, &fakeLauncher{}, 1)

	created, err := manager.Create(context.Background(), SessionSpec{
		ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatalf("空工作目录应回落到授权根目录: %v", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if created.CWD != canonicalRoot {
		t.Fatalf("终端工作目录 = %q, 期望 %q", created.CWD, canonicalRoot)
	}
}

func TestManagerListUsesStableCreationOrder(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	sequence := 0
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 2,
		BufferBytes: 1024, SnapshotBytes: 256, CloseGrace: 50 * time.Millisecond,
		IDGenerator: func() (string, error) {
			sequence++
			return fmt.Sprintf("terminal-%d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}

	values, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].SessionID != "terminal-1" || values[1].SessionID != "terminal-2" {
		t.Fatalf("终端列表顺序不稳定: %#v", values)
	}
}

func TestManagerIsolatesConcurrentSessionIOAndClose(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	sequence := 0
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 2,
		BufferBytes: 1024, SnapshotBytes: 256, CloseGrace: 50 * time.Millisecond,
		IDGenerator: func() (string, error) {
			sequence++
			return fmt.Sprintf("terminal-%d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}
	first, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	running := launcher.all()
	if len(running) != 2 {
		t.Fatalf("终端启动数量 = %d", len(running))
	}

	running[0].device.emit([]byte("first-only\n"))
	running[1].device.emit([]byte("second-only\n"))
	waitForTerminal(t, func() bool {
		firstSnapshot, firstErr := manager.Snapshot(context.Background(), first.SessionID, 0)
		secondSnapshot, secondErr := manager.Snapshot(context.Background(), second.SessionID, 0)
		return firstErr == nil && secondErr == nil && len(firstSnapshot.Frames) == 1 && len(secondSnapshot.Frames) == 1
	})

	firstSnapshot, err := manager.Snapshot(context.Background(), first.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := manager.Snapshot(context.Background(), second.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstSnapshot.Frames[0].Data) != "first-only\n" || string(secondSnapshot.Frames[0].Data) != "second-only\n" {
		t.Fatalf("并发终端输出串线: first=%q second=%q", firstSnapshot.Frames[0].Data, secondSnapshot.Frames[0].Data)
	}

	if err := manager.Write(context.Background(), second.SessionID, []byte("write-second\r")); err != nil {
		t.Fatal(err)
	}
	if got := string(running[0].device.written()); got != "" {
		t.Fatalf("写入 second 不得污染 first: %q", got)
	}
	if got := string(running[1].device.written()); got != "write-second\r" {
		t.Fatalf("second 写入 = %q", got)
	}

	if err := manager.CloseSession(context.Background(), second.SessionID, "close-second"); err != nil {
		t.Fatal(err)
	}
	firstInfo, err := manager.Get(context.Background(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if firstInfo.State != StateRunning {
		t.Fatalf("关闭 second 不得影响 first: %#v", firstInfo)
	}
	secondInfo, err := manager.Get(context.Background(), second.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if secondInfo.State != StateClosed || secondInfo.ExitCode == nil || *secondInfo.ExitCode != 0 {
		t.Fatalf("second 关闭状态错误: %#v", secondInfo)
	}
}

func TestManagerCloseSessionMarksEndedSessionsClosed(t *testing.T) {
	root := t.TempDir()
	for _, item := range []struct {
		name string
		code int
		err  error
		from string
	}{
		{name: "exited", code: 1, err: errors.New("exit status 1"), from: StateExited},
		{name: "interrupted", code: -1, err: errors.New("signal: killed"), from: StateInterrupted},
	} {
		t.Run(item.name, func(t *testing.T) {
			launcher := &fakeLauncher{}
			manager := newTestManager(t, root, launcher, 1)
			created, err := manager.Create(context.Background(), SessionSpec{
				CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "墨子引擎更新", Cols: 80, Rows: 24,
			})
			if err != nil {
				t.Fatal(err)
			}
			launcher.last().process.exit(item.code, item.err)
			waitForTerminal(t, func() bool {
				value, getErr := manager.Get(context.Background(), created.SessionID)
				return getErr == nil && value.State == item.from
			})
			if err := manager.CloseSession(context.Background(), created.SessionID, "user-close"); err != nil {
				t.Fatal(err)
			}
			closed, err := manager.Get(context.Background(), created.SessionID)
			if err != nil || closed.State != StateClosed {
				t.Fatalf("已结束会话关闭后应记为 closed: %#v %v", closed, err)
			}
			listed, err := manager.List(context.Background())
			if err != nil || len(listed) != 1 || listed[0].State != StateClosed {
				t.Fatalf("关闭后列表应保留 closed 记录: %#v %v", listed, err)
			}
			if err := manager.CloseSession(context.Background(), created.SessionID, "user-close-again"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManagerEnforcesConcurrentSessionLimitAndAllowsReplacementAfterExit(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	manager := newTestManager(t, root, launcher, 1)
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}

	first, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), spec); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("会话上限错误 = %v", err)
	}
	launcher.last().process.exit(7, errors.New("exit status 7"))
	waitForTerminal(t, func() bool {
		value, getErr := manager.Get(context.Background(), first.SessionID)
		return getErr == nil && value.State == StateExited
	})
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatalf("已退出会话不应占用并发配额: %v", err)
	}
}

func TestManagerUsesSharedResourcePoolForWholeSession(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	pool := newTerminalResourcePool(t, 1, 2)
	manager := newPooledTestManager(t, root, launcher, pool, 2)
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}

	first, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	secondResult := make(chan struct {
		info SessionInfo
		err  error
	}, 1)
	go func() {
		value, createErr := manager.Create(context.Background(), spec)
		secondResult <- struct {
			info SessionInfo
			err  error
		}{value, createErr}
	}()
	waitForTerminal(t, func() bool {
		snapshot := pool.Snapshot()
		return len(snapshot.Groups) == 1 && snapshot.Groups[0].Running == 1 && snapshot.Groups[0].Queued == 1
	})
	if launcher.count() != 1 {
		t.Fatalf("资源租约排队期间启动了额外终端: %d", launcher.count())
	}
	if err := manager.CloseSession(context.Background(), first.SessionID, "release-resource"); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-secondResult:
		if second.err != nil || second.info.State != StateRunning {
			t.Fatalf("资源释放后的终端创建失败: info=%#v err=%v", second.info, second.err)
		}
		if err := manager.CloseSession(context.Background(), second.info.SessionID, "test-complete"); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("资源释放后排队终端未启动")
	}
}

func TestManagerReportsUnavailableSharedResourcePool(t *testing.T) {
	root := t.TempDir()
	pool, err := resourcepool.New([]resourcepool.GroupConfig{{
		Name: resourcepool.GroupTerminal, Workers: 1, QueueCapacity: 1,
	}}, resourcepool.Options{Name: "terminal-unstarted"})
	if err != nil {
		t.Fatal(err)
	}
	manager := newPooledTestManager(t, root, &fakeLauncher{}, pool, 1)
	_, err = manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24,
	})
	if !errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("未启动资源池错误 = %v", err)
	}
}

func TestManagerReleasesResourceLeaseAfterLaunchFailure(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{launchErr: errors.New("launch failed")}
	pool := newTerminalResourcePool(t, 1, 1)
	manager := newPooledTestManager(t, root, launcher, pool, 1)
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24}

	if _, err := manager.Create(context.Background(), spec); err == nil {
		t.Fatal("终端启动失败必须返回错误")
	}
	waitForTerminal(t, func() bool {
		snapshot := pool.Snapshot().Groups[0]
		return snapshot.Running == 0 && snapshot.Queued == 0
	})
	launcher.setError(nil)
	created, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("启动失败释放资源后应可重试: %v", err)
	}
	if err := manager.CloseSession(context.Background(), created.SessionID, "test-complete"); err != nil {
		t.Fatal(err)
	}
}

func TestManagerTerminatesSessionWhenResourcePoolCloses(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	pool := newTerminalResourcePool(t, 1, 1)
	manager := newPooledTestManager(t, root, launcher, pool, 1)
	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, func() bool {
		info, getErr := manager.Get(context.Background(), created.SessionID)
		return getErr == nil && terminalState(info.State)
	})
	running := launcher.last()
	running.process.mu.Lock()
	kills := running.process.kills
	running.process.mu.Unlock()
	if kills != 1 {
		t.Fatalf("资源租约撤销后终端进程树关闭次数 = %d", kills)
	}
}

func TestManagerRejectsSymlinkEscapingGrantedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前平台不支持测试符号链接: %v", err)
	}
	manager := newTestManager(t, root, &fakeLauncher{}, 1)
	_, err := manager.Create(context.Background(), SessionSpec{
		CWD: link, ShellProfile: "system", Scope: "workspace-terminal", Cols: 80, Rows: 24,
	})
	if !errors.Is(err, ErrPathNotGranted) {
		t.Fatalf("越界符号链接错误 = %v", err)
	}
}

func newTestManager(t *testing.T, root string, launcher Launcher, maxSessions int) *Manager {
	t.Helper()
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: maxSessions,
		BufferBytes: 1024, SnapshotBytes: 256, CloseGrace: 50 * time.Millisecond,
		IDGenerator: func() (string, error) { return "terminal-test-" + time.Now().Format("150405.000000000"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager
}

func newPooledTestManager(t *testing.T, root string, launcher Launcher, pool *resourcepool.Pool, maxSessions int) *Manager {
	t.Helper()
	var sequence atomic.Int64
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: maxSessions,
		BufferBytes: 1024, SnapshotBytes: 256, CloseGrace: 50 * time.Millisecond, ResourcePool: pool,
		IDGenerator: func() (string, error) { return fmt.Sprintf("terminal-pooled-%d", sequence.Add(1)), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager
}

func newTerminalResourcePool(t *testing.T, workers, queue int) *resourcepool.Pool {
	t.Helper()
	pool, err := resourcepool.New([]resourcepool.GroupConfig{{
		Name: resourcepool.GroupTerminal, Workers: workers, QueueCapacity: queue,
	}}, resourcepool.Options{Name: "terminal-test-pool"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.Close(ctx); err != nil {
			t.Errorf("关闭终端资源池: %v", err)
		}
	})
	return pool
}

func waitForTerminal(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待终端状态超时")
}

type fakeLauncher struct {
	mu        sync.Mutex
	sessions  []*fakeRunning
	launchErr error
}

func (launcher *fakeLauncher) Launch(_ context.Context, request LaunchRequest) (Device, Process, error) {
	launcher.mu.Lock()
	err := launcher.launchErr
	launcher.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	device := newFakeDevice(request.Cols, request.Rows)
	process := newFakeProcess()
	running := &fakeRunning{device: device, process: process, request: request}
	launcher.mu.Lock()
	launcher.sessions = append(launcher.sessions, running)
	launcher.mu.Unlock()
	return device, process, nil
}

func (launcher *fakeLauncher) setError(err error) {
	launcher.mu.Lock()
	launcher.launchErr = err
	launcher.mu.Unlock()
}

func (launcher *fakeLauncher) last() *fakeRunning {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return launcher.sessions[len(launcher.sessions)-1]
}

func (launcher *fakeLauncher) count() int {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return len(launcher.sessions)
}

func (launcher *fakeLauncher) all() []*fakeRunning {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return append([]*fakeRunning(nil), launcher.sessions...)
}

type blockingLauncher struct {
	started chan struct{}
}

func (launcher *blockingLauncher) Launch(ctx context.Context, _ LaunchRequest) (Device, Process, error) {
	if launcher.started != nil {
		select {
		case <-launcher.started:
		default:
			close(launcher.started)
		}
	}
	<-ctx.Done()
	return nil, nil, ctx.Err()
}

type fakeRunning struct {
	device  *fakeDevice
	process *fakeProcess
	request LaunchRequest
}

type fakeDevice struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	mu     sync.Mutex
	input  []byte
	cols   int
	rows   int
}

func newFakeDevice(cols, rows int) *fakeDevice {
	reader, writer := io.Pipe()
	return &fakeDevice{reader: reader, writer: writer, cols: cols, rows: rows}
}

func (device *fakeDevice) Read(data []byte) (int, error) { return device.reader.Read(data) }
func (*fakeDevice) SetWriteDeadline(time.Time) error     { return nil }
func (device *fakeDevice) Write(data []byte) (int, error) {
	device.mu.Lock()
	defer device.mu.Unlock()
	device.input = append(device.input, data...)
	return len(data), nil
}
func (device *fakeDevice) Resize(cols, rows int) error {
	device.mu.Lock()
	defer device.mu.Unlock()
	device.cols, device.rows = cols, rows
	return nil
}
func (device *fakeDevice) Close() error {
	return errors.Join(device.reader.Close(), device.writer.Close())
}
func (device *fakeDevice) emit(data []byte) { _, _ = device.writer.Write(data) }
func (device *fakeDevice) written() []byte {
	device.mu.Lock()
	defer device.mu.Unlock()
	return append([]byte(nil), device.input...)
}

type processResult struct {
	code int
	err  error
}

type fakeProcess struct {
	mu           sync.Mutex
	interrupts   int
	kills        int
	waitReturned bool
	result       chan processResult
	once         sync.Once
}

func newFakeProcess() *fakeProcess { return &fakeProcess{result: make(chan processResult, 1)} }
func (*fakeProcess) PID() int      { return 42 }
func (process *fakeProcess) Wait() (int, error) {
	result := <-process.result
	process.mu.Lock()
	process.waitReturned = true
	process.mu.Unlock()
	return result.code, result.err
}
func (process *fakeProcess) Interrupt() error {
	process.mu.Lock()
	process.interrupts++
	process.mu.Unlock()
	process.exit(0, nil)
	return nil
}
func (process *fakeProcess) KillTree() error {
	process.mu.Lock()
	process.kills++
	process.mu.Unlock()
	process.exit(137, errors.New("killed"))
	return nil
}
func (process *fakeProcess) exit(code int, err error) {
	process.once.Do(func() { process.result <- processResult{code: code, err: err} })
}
