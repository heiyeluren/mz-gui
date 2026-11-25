package terminal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestUnlimitedSessionsConcurrentCreateBeyond32(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(context.Background(), ManagerOptions{Launcher: &fakeLauncher{}, AllowedRoots: []string{root}, BufferBytes: 1024, CloseGrace: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	spec := SessionSpec{CWD: root, Scope: "unlimited-test", ShellProfile: "system", Cols: 80, Rows: 24}
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.Create(context.Background(), spec); err != nil {
				t.Errorf("不限模式新建终端失败: %v", err)
			}
		}()
	}
	wg.Wait()
	all, err := manager.List(context.Background())
	if err != nil || len(all) != 40 {
		t.Fatalf("期望 40 个活跃终端，实际 %d: %v", len(all), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Create(ctx, spec); !errors.Is(err, context.Canceled) {
		t.Fatalf("不限数量仍需遵守取消: %v", err)
	}
}

func TestUnlimitedSessionsProtectTailWithoutBlockingAndPruneExpired(t *testing.T) {
	manager, launcher, clock, spec := retentionFixture(t)
	manager.maxSessions = 0
	ids := retainEnded(t, manager, launcher, spec, 40)
	for _, id := range ids {
		value, err := manager.Snapshot(context.Background(), id, 0)
		if err != nil || len(value.Frames) != 1 {
			t.Fatalf("新建终端丢失保留窗口内的尾部: %s %v", id, err)
		}
	}
	active, err := manager.Create(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(3 * time.Minute)
	if _, err = manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	all, _ := manager.List(context.Background())
	if len(all) != manager.maxRetainedSessions+2 {
		t.Fatalf("已到期历史未回收，实际 %d", len(all))
	}
	if info, err := manager.Get(context.Background(), active.SessionID); err != nil || info.State != StateRunning {
		t.Fatal("回收了活跃终端")
	}
}

func TestUnlimitedSessionsNeverEvictUndrainedOutput(t *testing.T) {
	manager, launcher, clock, spec := retentionFixture(t)
	manager.maxSessions = 0
	ids := retainEnded(t, manager, launcher, spec, 17)
	old, _ := manager.session(ids[0])
	readerDone := make(chan struct{})
	old.readerDone = readerDone // 故障注入：进程已退出，设备末帧仍在传输。
	clock.advance(3 * time.Minute)
	if _, err := manager.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Snapshot(context.Background(), ids[0], 0); err != nil {
		t.Fatal("未排空输出的会话被回收")
	}
	close(readerDone)
}
