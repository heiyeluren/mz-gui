package terminal

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestInputRevisionRejectsUserInputWithoutEcho(t *testing.T) {
	device := newInputTestDevice()
	manager, created := newInputTestManager(t, device)
	if created.InputRevision != 0 {
		t.Fatal("新终端不应已有输入")
	}
	if err := manager.Write(context.Background(), created.SessionID, []byte("用户")); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Snapshot(context.Background(), created.SessionID, 0)
	if err != nil || len(snapshot.Frames) != 0 || snapshot.Session.InputRevision != 1 {
		t.Fatalf("尚未回显的用户输入也必须推进版本: %#v %v", snapshot, err)
	}
	// 用户随后删空输入，也不能使先前的自动投递凭证重新有效。
	if err := manager.Write(context.Background(), created.SessionID, []byte("\x7f\x7f")); err != nil {
		t.Fatal(err)
	}
	got, err := manager.WriteIfInputUnchanged(context.Background(), created.SessionID, created.InputRevision, []byte("自动输入\r"))
	if !errors.Is(err, ErrInputChanged) || got != 2 || device.input() != "用户\x7f\x7f" {
		t.Fatalf("旧版本必须零写入: revision=%d input=%q err=%v", got, device.input(), err)
	}
	if err := manager.Resize(context.Background(), created.SessionID, 90, 30); err != nil {
		t.Fatal(err)
	}
	info, _ := manager.Get(context.Background(), created.SessionID)
	if info.InputRevision != 2 {
		t.Fatal("调整尺寸不能改变输入版本")
	}
}

func TestInputRevisionConcurrentConditionalWriteOnlyOneWins(t *testing.T) {
	device := newInputTestDevice()
	manager, session := newInputTestManager(t, device)
	start := make(chan struct{})
	results := make(chan error, 16)
	for range 16 {
		go func() {
			<-start
			_, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 0, []byte("一份\r"))
			results <- err
		}()
	}
	close(start)
	winners := 0
	for range 16 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrInputChanged) {
			t.Fatal(err)
		}
	}
	if winners != 1 || device.input() != "一份\r" {
		t.Fatalf("条件写必须单次生效: winners=%d input=%q", winners, device.input())
	}
}

func TestInputRevisionCanceledBeforeWriteDoesNotConsumeVersion(t *testing.T) {
	device := newInputTestDevice()
	manager, session := newInputTestManager(t, device)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.WriteIfInputUnchanged(ctx, session.SessionID, 0, []byte("不应发送")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 0, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	info, _ := manager.Get(context.Background(), session.SessionID)
	if info.InputRevision != 0 || device.input() != "" {
		t.Fatal("调用前已取消或参数无效不得消费输入版本")
	}
}

func TestInputRevisionCanceledBackgroundWriteKeepsExclusiveOwnership(t *testing.T) {
	device := newInputTestDevice()
	release := make(chan struct{})
	device.release = release
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	manager, session := newInputTestManager(t, device)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		revision uint64
		err      error
	}
	resultCh := make(chan result, 1)
	data := []byte("原输入")
	go func() {
		revision, err := manager.WriteIfInputUnchanged(ctx, session.SessionID, 0, data)
		resultCh <- result{revision, err}
	}()
	select {
	case <-device.started:
	case <-time.After(time.Second):
		t.Fatal("设备写入未开始")
	}
	// 在 I/O 排他锁外排队也必须服从自身 context，且不消费版本。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()
	if _, err := manager.WriteIfInputUnchanged(waitCtx, session.SessionID, 1, []byte("排队输入")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("锁等待未服从context: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		_, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("长期排队输入"))
		queued <- err
	}()
	cancel()
	select {
	case result := <-resultCh:
		if result.revision != 1 || !errors.Is(result.err, context.Canceled) || !errors.Is(result.err, ErrInputUncertain) {
			t.Fatalf("已进入设备的写入不能声明未发送: %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("无deadline设备阻塞了已取消调用")
	}
	select {
	case err := <-queued:
		if !errors.Is(err, ErrInputUncertain) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("已经排队的写入未获知uncertain状态")
	}
	// 调用方已返回，可以复用原缓冲；后台设备必须持有自己的副本。
	copy(data, []byte("新内容"))
	if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("不得并发")); !errors.Is(err, ErrInputUncertain) {
		t.Fatalf("后台写未结束时条件写必须明确拒绝: %v", err)
	}
	if err := manager.Write(context.Background(), session.SessionID, []byte("用户后续输入")); !errors.Is(err, ErrInputUncertain) {
		t.Fatalf("普通写也不能与后台写并发: %v", err)
	}
	info, _ := manager.Get(context.Background(), session.SessionID)
	if info.InputRevision != 1 {
		t.Fatalf("排队取消和uncertain拒绝不能增量: %d", info.InputRevision)
	}
	releaseOnce.Do(func() { close(release) })
	waitForTerminal(t, func() bool {
		_, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("明确的新输入"))
		if err != nil && !errors.Is(err, ErrInputUncertain) {
			t.Fatal(err)
		}
		return err == nil
	})
	if device.input() != "原输入明确的新输入" || device.maxConcurrent() != 1 {
		t.Fatalf("后台输入被覆盖或发生并发: %q max=%d", device.input(), device.maxConcurrent())
	}
	if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 0, []byte("重发")); !errors.Is(err, ErrInputChanged) {
		t.Fatal("已取消但可能发送的旧版本不能重放")
	}
}

func TestInputRevisionPartialWriteIsNotSuccessfulOrReplayable(t *testing.T) {
	deviceError := errors.New("测试设备故障")
	for _, test := range []struct {
		name               string
		count              int
		deviceErr, wantErr error
	}{
		{"zero-short", 0, nil, io.ErrShortWrite},
		{"partial-short", 2, nil, io.ErrShortWrite},
		{"zero-error", 0, deviceError, deviceError},
		{"partial-error", 2, deviceError, deviceError},
		{"full-error", 6, deviceError, deviceError},
	} {
		t.Run(test.name, func(t *testing.T) {
			device := newInputTestDevice()
			device.short, device.err = test.count, test.deviceErr
			manager, session := newInputTestManager(t, device)
			revision, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 0, []byte("abcdef"))
			if revision != 1 || !errors.Is(err, test.wantErr) || test.count > 0 && !errors.Is(err, ErrInputUncertain) {
				t.Fatalf("部分写不能冒充成功: revision=%d err=%v", revision, err)
			}
			if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 0, []byte("abcdef")); !errors.Is(err, ErrInputChanged) {
				t.Fatal("部分写不能使用旧版本重发")
			}
			if len(device.input()) != test.count {
				t.Fatal("发生了重复输入")
			}
		})
	}
}

func TestInputRevisionCloseDoesNotWaitForOrReviveBackgroundWrite(t *testing.T) {
	device := newInputTestDevice()
	release := make(chan struct{})
	device.release = release
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	manager, session := newInputTestManager(t, device)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Write(ctx, session.SessionID, []byte("已尝试")) }()
	<-device.started
	cancel()
	if err := <-done; !errors.Is(err, ErrInputUncertain) {
		t.Fatal(err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
	defer closeCancel()
	if err := manager.CloseSession(closeCtx, session.SessionID, "test"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("不能复活")); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("结束会话不得恢复写入: %v", err)
	}
}

type inputTestLauncher struct {
	device  Device
	process *fakeProcess
}

func (l inputTestLauncher) Launch(context.Context, LaunchRequest) (Device, Process, error) {
	return l.device, l.process, nil
}

func newInputTestManager(t *testing.T, device Device) (*Manager, SessionInfo) {
	t.Helper()
	root := t.TempDir()
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: inputTestLauncher{device, newFakeProcess()}, AllowedRoots: []string{root}, MaxSessions: 1, CloseGrace: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	session, err := manager.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "input-test", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	return manager, session
}

// 不实现 SetWriteDeadline，专门验证调用方返回后仍存活的设备写。
type inputTestDevice struct {
	mu              sync.Mutex
	closed          chan struct{}
	closeOnce       sync.Once
	started         chan struct{}
	release         <-chan struct{}
	written         []byte
	short           int
	err             error
	active, maximum int
}

func newInputTestDevice() *inputTestDevice {
	return &inputTestDevice{closed: make(chan struct{}), started: make(chan struct{}, 16), short: -1}
}
func (d *inputTestDevice) Read([]byte) (int, error) { <-d.closed; return 0, io.EOF }
func (d *inputTestDevice) Resize(int, int) error    { return nil }
func (d *inputTestDevice) Close() error             { d.closeOnce.Do(func() { close(d.closed) }); return nil }
func (d *inputTestDevice) Write(data []byte) (int, error) {
	d.mu.Lock()
	d.active++
	if d.active > d.maximum {
		d.maximum = d.active
	}
	d.mu.Unlock()
	select {
	case d.started <- struct{}{}:
	default:
	}
	if d.release != nil {
		<-d.release
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	count := len(data)
	if d.short >= 0 {
		count = d.short
	}
	d.written = append(d.written, data[:count]...)
	d.active--
	return count, d.err
}
func (d *inputTestDevice) input() string      { d.mu.Lock(); defer d.mu.Unlock(); return string(d.written) }
func (d *inputTestDevice) maxConcurrent() int { d.mu.Lock(); defer d.mu.Unlock(); return d.maximum }

func TestInputRevisionDeadlineCleanupKeepsIOOwnership(t *testing.T) {
	device := &inputDeadlineTestDevice{inputTestDevice: newInputTestDevice(), callbackEntered: make(chan struct{}), releaseCallback: make(chan struct{}), reset: make(chan struct{})}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(device.releaseCallback) }) })
	manager, session := newInputTestManager(t, device)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := manager.WriteIfInputUnchanged(ctx, session.SessionID, 0, []byte("首次输入"))
		done <- err
	}()
	<-device.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrInputUncertain) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline回调尚未返回时调用方取消没有及时返回")
	}
	if _, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("不能提前写")); !errors.Is(err, ErrInputUncertain) {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(device.releaseCallback) })
	select {
	case <-device.reset:
	case <-time.After(time.Second):
		t.Fatal("deadline回调未先结束再清理deadline")
	}
	waitForTerminal(t, func() bool {
		_, err := manager.WriteIfInputUnchanged(context.Background(), session.SessionID, 1, []byte("清理后新输入"))
		if err != nil && !errors.Is(err, ErrInputUncertain) {
			t.Fatal(err)
		}
		return err == nil
	})
	if device.input() != "首次输入清理后新输入" {
		t.Fatal(device.input())
	}
}

type inputDeadlineTestDevice struct {
	*inputTestDevice
	callbackEntered, releaseCallback, reset chan struct{}
	callbackOnce, resetOnce                 sync.Once
}

func (d *inputDeadlineTestDevice) Write(data []byte) (int, error) {
	n, err := d.inputTestDevice.Write(data)
	<-d.callbackEntered
	return n, err
}

func (d *inputDeadlineTestDevice) SetWriteDeadline(deadline time.Time) error {
	if !deadline.IsZero() && deadline.Before(time.Now()) {
		d.callbackOnce.Do(func() { close(d.callbackEntered) })
		<-d.releaseCallback
	}
	if deadline.IsZero() {
		// 即使Write已经返回，也不能抢先清理，否则迟到回调会污染下一次写入。
		select {
		case <-d.releaseCallback:
			d.resetOnce.Do(func() { close(d.reset) })
		default:
			return errors.New("deadline回调尚未结束")
		}
	}
	return nil
}
