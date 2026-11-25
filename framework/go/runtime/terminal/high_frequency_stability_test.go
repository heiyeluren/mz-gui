package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagerHighFrequencyOutputStaysBoundedAndWritable(t *testing.T) {
	root := t.TempDir()
	launcher := &floodLauncher{}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 4 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "高频输出", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	running := launcher.last()

	waitForTerminal(t, func() bool {
		return running.device.produced() >= 12<<20 && sessionRingUsed(manager, created.SessionID) > 0
	})

	used := sessionRingUsed(manager, created.SessionID)
	if used > 4<<20 {
		t.Fatalf("高频输出后 ring 越界: used=%d", used)
	}

	var cursor, last uint64
	seenTruncated := false
	for i := 0; i < 80; i++ {
		snapshot, snapshotErr := manager.Snapshot(context.Background(), created.SessionID, cursor)
		if snapshotErr != nil {
			t.Fatalf("高压下快照失败: %v", snapshotErr)
		}
		if snapshot.NextCursor < last {
			t.Fatalf("快照游标回退: last=%d next=%d truncated=%v", last, snapshot.NextCursor, snapshot.Truncated)
		}
		if snapshot.Truncated {
			seenTruncated = true
		}
		last = snapshot.NextCursor
		cursor = snapshot.NextCursor
		time.Sleep(time.Millisecond)
	}
	if !seenTruncated {
		t.Fatal("远超 4MiB 后快照必须报告截断")
	}

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancelWrite()
	if err := manager.Write(writeCtx, created.SessionID, []byte("pwd\r")); err != nil {
		t.Fatalf("高压下输入不可写: %v", err)
	}
	if got := running.device.written(); !bytes.Contains(got, []byte("pwd\r")) {
		t.Fatalf("高压下输入未送达: %q", got)
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := manager.CloseSession(closeCtx, created.SessionID, "high-frequency-stop"); err != nil {
		t.Fatalf("关闭高频会话失败: %v", err)
	}
	waitForTerminal(t, func() bool {
		return running.device.readStopped() && running.process.waited()
	})
}

func TestManagerWriteReturnsWhenContextExpiresDuringBlockedDeviceWrite(t *testing.T) {
	root := t.TempDir()
	launcher := &blockWriteLauncher{}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 4 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "阻塞写入", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancelWrite()
	done := make(chan error, 1)
	go func() {
		done <- manager.Write(writeCtx, created.SessionID, []byte("blocked\r"))
	}()
	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("短 ctx 下阻塞写入永久卡住，未返回")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("短 ctx 下阻塞写入应返回截止错误, 得到 %v", err)
	}
}

func TestManagerCloseStopsOutputReaderAndWait(t *testing.T) {
	root := t.TempDir()
	launcher := &floodLauncher{}
	manager, err := NewManager(context.Background(), ManagerOptions{
		Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1,
		BufferBytes: 4 << 20, SnapshotBytes: MaxInputBytes, CloseGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := manager.Create(context.Background(), SessionSpec{
		CWD: root, ShellProfile: "system", Scope: "workspace-terminal", Title: "关闭回收", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	running := launcher.last()
	waitForTerminal(t, func() bool { return running.device.produced() >= 1<<20 })

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := manager.Close(closeCtx); err != nil {
		t.Fatalf("Manager.Close 应在超时前返回: %v", err)
	}
	waitForTerminal(t, func() bool {
		return running.device.readStopped() && running.process.waited()
	})
	info, err := manager.Get(context.Background(), created.SessionID)
	if err != nil || !terminalState(info.State) {
		t.Fatalf("关闭后会话应进入终态: %#v err=%v", info, err)
	}
}

func sessionRingUsed(manager *Manager, sessionID string) int {
	manager.mu.RLock()
	session := manager.sessions[sessionID]
	manager.mu.RUnlock()
	if session == nil || session.output == nil {
		return 0
	}
	return ringUsed(session.output)
}

type floodLauncher struct {
	mu       sync.Mutex
	sessions []*floodRunning
}

type floodRunning struct {
	device  *floodDevice
	process *fakeProcess
}

func (launcher *floodLauncher) Launch(_ context.Context, request LaunchRequest) (Device, Process, error) {
	device := newFloodDevice(request.Cols, request.Rows)
	process := newFakeProcess()
	running := &floodRunning{device: device, process: process}
	launcher.mu.Lock()
	launcher.sessions = append(launcher.sessions, running)
	launcher.mu.Unlock()
	return device, process, nil
}

func (launcher *floodLauncher) last() *floodRunning {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return launcher.sessions[len(launcher.sessions)-1]
}

type floodDevice struct {
	mu        sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once
	input     []byte
	bytesOut  atomic.Int64
	stopped   atomic.Bool
	cols      int
	rows      int
}

func newFloodDevice(cols, rows int) *floodDevice {
	return &floodDevice{closed: make(chan struct{}), cols: cols, rows: rows}
}

func (device *floodDevice) Read(data []byte) (int, error) {
	select {
	case <-device.closed:
		device.stopped.Store(true)
		return 0, io.EOF
	default:
	}
	if len(data) == 0 {
		return 0, nil
	}
	n := len(data)
	if n > 32<<10 {
		n = 32 << 10
	}
	for i := 0; i < n; i++ {
		data[i] = 'X'
	}
	produced := device.bytesOut.Add(int64(n))
	if produced > 16<<20 {
		select {
		case <-device.closed:
			device.stopped.Store(true)
			return 0, io.EOF
		case <-time.After(50 * time.Microsecond):
		}
	} else {
		runtime.Gosched()
	}
	return n, nil
}

func (device *floodDevice) Write(data []byte) (int, error) {
	select {
	case <-device.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	device.mu.Lock()
	device.input = append(device.input, data...)
	device.mu.Unlock()
	return len(data), nil
}

func (*floodDevice) SetWriteDeadline(time.Time) error { return nil }

func (device *floodDevice) Resize(cols, rows int) error {
	device.mu.Lock()
	device.cols, device.rows = cols, rows
	device.mu.Unlock()
	return nil
}

func (device *floodDevice) Close() error {
	device.closeOnce.Do(func() { close(device.closed) })
	return nil
}

func (device *floodDevice) produced() int64 { return device.bytesOut.Load() }
func (device *floodDevice) readStopped() bool {
	select {
	case <-device.closed:
		return device.stopped.Load()
	default:
		return device.stopped.Load()
	}
}
func (device *floodDevice) written() []byte {
	device.mu.Lock()
	defer device.mu.Unlock()
	return append([]byte(nil), device.input...)
}

type blockWriteLauncher struct {
	mu     sync.Mutex
	device *blockWriteDevice
}

func (launcher *blockWriteLauncher) Launch(_ context.Context, request LaunchRequest) (Device, Process, error) {
	device := newBlockWriteDevice(request.Cols, request.Rows)
	launcher.mu.Lock()
	launcher.device = device
	launcher.mu.Unlock()
	return device, newFakeProcess(), nil
}

type blockWriteDevice struct {
	mu         sync.Mutex
	closed     chan struct{}
	closeOnce  sync.Once
	deadlineCh chan struct{}
	deadline   time.Time
	cols       int
	rows       int
}

func newBlockWriteDevice(cols, rows int) *blockWriteDevice {
	return &blockWriteDevice{
		closed: make(chan struct{}), deadlineCh: make(chan struct{}, 1), cols: cols, rows: rows,
	}
}

func (device *blockWriteDevice) Read(_ []byte) (int, error) {
	<-device.closed
	return 0, io.EOF
}

func (device *blockWriteDevice) Write(data []byte) (int, error) {
	for {
		device.mu.Lock()
		deadline := device.deadline
		device.mu.Unlock()
		if !deadline.IsZero() && !deadline.After(time.Now()) {
			return 0, os.ErrDeadlineExceeded
		}
		select {
		case <-device.closed:
			return 0, io.ErrClosedPipe
		case <-device.deadlineCh:
			continue
		case <-time.After(20 * time.Millisecond):
			continue
		}
	}
}

func (device *blockWriteDevice) SetWriteDeadline(deadline time.Time) error {
	device.mu.Lock()
	device.deadline = deadline
	device.mu.Unlock()
	select {
	case device.deadlineCh <- struct{}{}:
	default:
	}
	return nil
}

func (device *blockWriteDevice) Resize(cols, rows int) error {
	device.mu.Lock()
	device.cols, device.rows = cols, rows
	device.mu.Unlock()
	return nil
}

func (device *blockWriteDevice) Close() error {
	device.closeOnce.Do(func() { close(device.closed) })
	return nil
}

func (process *fakeProcess) waited() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.waitReturned
}
