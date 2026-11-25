package terminal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type retentionClock struct{ value atomic.Int64 }

func (clock *retentionClock) now() time.Time              { return time.Unix(0, clock.value.Load()).UTC() }
func (clock *retentionClock) advance(value time.Duration) { clock.value.Add(int64(value)) }

func retentionFixture(t *testing.T) (*Manager, *fakeLauncher, *retentionClock, SessionSpec) {
	t.Helper()
	clock := &retentionClock{}
	clock.value.Store(time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC).UnixNano())
	launcher := &fakeLauncher{}
	root := t.TempDir()
	var sequence atomic.Int64
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 8, BufferBytes: 1024, SnapshotBytes: 256, CloseGrace: time.Millisecond, Now: clock.now, IDGenerator: func() (string, error) { return fmt.Sprintf("retained-%03d", sequence.Add(1)), nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	return manager, launcher, clock, SessionSpec{CWD: root, ShellProfile: "system", Scope: "retention-test", Cols: 80, Rows: 24}
}

func retainEnded(t *testing.T, manager *Manager, launcher *fakeLauncher, spec SessionSpec, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	for range count {
		info, err := manager.Create(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		launcher.last().device.emit([]byte("final:" + info.SessionID))
		if err := manager.CloseSession(context.Background(), info.SessionID, "fixture-finished"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, info.SessionID)
	}
	return ids
}

func TestManagerRetentionBoundsDefault24AndProtectsFreshTail(t *testing.T) {
	manager, launcher, clock, spec := retentionFixture(t)
	ids := retainEnded(t, manager, launcher, spec, 24)
	for _, elapsed := range []time.Duration{0, 2*time.Minute - time.Nanosecond} {
		clock.advance(elapsed)
		if _, err := manager.Create(context.Background(), spec); !errors.Is(err, ErrSessionLimit) || !strings.Contains(err.Error(), "保留") {
			t.Fatalf("终态缓存保护期内应明确拒绝第25个会话: %v", err)
		}
		if launcher.count() != 24 {
			t.Fatal("容量拒绝后仍启动了进程")
		}
	}
	for _, id := range ids {
		value, err := manager.Snapshot(context.Background(), id, 0)
		if err != nil || value.Session.State != StateClosed || len(value.Frames) != 1 || string(value.Frames[0].Data) != "final:"+id {
			t.Fatalf("刚结束的最终输出丢失: %s %#v %v", id, value, err)
		}
	}
	clock.advance(time.Nanosecond)
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatalf("保留窗口到期未恢复容量: %v", err)
	}
	all, err := manager.List(context.Background())
	if err != nil || len(all) != 24 {
		t.Fatalf("总存量未保持24上界: %d %v", len(all), err)
	}
}

func TestManagerRetentionNeverEvictsActiveSessions(t *testing.T) {
	manager, launcher, clock, spec := retentionFixture(t)
	old := retainEnded(t, manager, launcher, spec, 16)
	clock.advance(3 * time.Minute)
	active := []string{}
	for range 8 {
		info, err := manager.Create(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		active = append(active, info.SessionID)
	}
	if _, err := manager.Create(context.Background(), spec); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("活跃并发上限未保留: %v", err)
	}
	if err := manager.CloseSession(context.Background(), active[0], "fixture-close-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	for _, id := range active[1:] {
		info, err := manager.Get(context.Background(), id)
		if err != nil || info.State != StateRunning {
			t.Fatalf("回收了活跃PTY: %s %v", id, err)
		}
	}
	if info, err := manager.Get(context.Background(), active[0]); err != nil || info.State != StateClosed {
		t.Fatal("回收了刚退出的PTY")
	}
	if _, err := manager.Get(context.Background(), old[0]); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("未回收最旧的合格终态: %v", err)
	}
}

func TestManagerRetentionWaitsForReaderDone(t *testing.T) {
	manager, launcher, clock, spec := retentionFixture(t)
	old := retainEnded(t, manager, launcher, spec, 1)[0]
	session, _ := manager.session(old)
	// 故障注入：进程终态已到，但设备尾部读取仍未完成。
	readerDone := make(chan struct{})
	session.readerDone = readerDone
	clock.advance(time.Minute)
	retainEnded(t, manager, launcher, spec, 23)
	clock.advance(time.Minute + time.Second)
	if _, err := manager.Create(context.Background(), spec); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("reader未完成就回收: %v", err)
	}
	if _, err := manager.Snapshot(context.Background(), old, 0); err != nil {
		t.Fatal("未读完的输出丢失")
	}
	close(readerDone)
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatalf("reader完成后未释放容量: %v", err)
	}
	if _, err := manager.Get(context.Background(), old); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("到期已读完会话没有被回收: %v", err)
	}
}

func TestManagerRetentionUsesReadLRUAndListDoesNotPinHistory(t *testing.T) {
	for _, read := range []string{"get", "snapshot"} {
		t.Run(read, func(t *testing.T) {
			manager, launcher, clock, spec := retentionFixture(t)
			ids := retainEnded(t, manager, launcher, spec, 24)
			clock.advance(3 * time.Minute)
			if read == "get" {
				_, _ = manager.Get(context.Background(), ids[0])
			} else {
				_, _ = manager.Snapshot(context.Background(), ids[0], 0)
			}
			clock.advance(time.Second)
			_, _ = manager.List(context.Background())
			if _, err := manager.Create(context.Background(), spec); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Get(context.Background(), ids[0]); err != nil {
				t.Fatal("最近访问的旧会话先被回收")
			}
			if _, err := manager.Get(context.Background(), ids[1]); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("列表查询改变了LRU排序: %v", err)
			}
		})
	}
}

func TestManagerRetentionDefaultsAndInvalidOptions(t *testing.T) {
	manager, launcher, _, spec := retentionFixture(t)
	if manager.maxSessions != 8 || manager.maxRetainedSessions != 16 || manager.completedRetentionGrace != 2*time.Minute {
		t.Fatal("零值选项改变了既有活跃容量或默认终态保留窗口")
	}
	for _, options := range []ManagerOptions{
		{MaxRetainedSessions: -1}, {MaxRetainedSessions: 257}, {CompletedRetentionGrace: -time.Second},
	} {
		options.Launcher, options.AllowedRoots, options.MaxSessions = launcher, []string{spec.CWD}, 8
		if _, err := NewManager(context.Background(), options); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法缓存选项未拒绝: %#v %v", options, err)
		}
	}
	custom, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{spec.CWD}, MaxSessions: 8, MaxRetainedSessions: 2, CompletedRetentionGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer custom.Close(context.Background())
	if custom.maxSessions != 8 || custom.maxRetainedSessions != 2 || custom.completedRetentionGrace != time.Second {
		t.Fatal("显式缓存选项未生效")
	}
}

func TestManagerRetentionConcurrentCreatesCannotExceedTotalBound(t *testing.T) {
	manager, launcher, _, spec := retentionFixture(t)
	retainEnded(t, manager, launcher, spec, 23)
	var started atomic.Int64
	var rejected atomic.Int64
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := manager.Create(context.Background(), spec); err == nil {
				started.Add(1)
			} else if errors.Is(err, ErrSessionRetentionBusy) {
				rejected.Add(1)
			} else {
				t.Errorf("容量竞争返回非预期错误: %v", err)
			}
		}()
	}
	group.Wait()
	all, err := manager.List(context.Background())
	if err != nil || len(all) != 24 || started.Load() != 1 || rejected.Load() != 7 || launcher.count() != 24 {
		t.Fatalf("并发预留越过24上界: count=%d started=%d rejected=%d err=%v", len(all), started.Load(), rejected.Load(), err)
	}
}
