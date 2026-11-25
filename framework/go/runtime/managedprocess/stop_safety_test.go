package managedprocess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStopSafetyCanceledRequestDoesNotSignal(t *testing.T) {
	for _, disable := range []bool{false, true} {
		p := newScriptedProcess(901)
		manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
		if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var err error
		if disable {
			err = manager.Disable(ctx)
		} else {
			err = manager.Stop(ctx)
		}
		if !errors.Is(err, context.Canceled) || p.Interrupted() || !manager.Status().Running {
			t.Errorf("取消请求不得触发停止: disable=%v err=%v interrupted=%v state=%#v", disable, err, p.Interrupted(), manager.Status())
		}
		p.Exit(0, nil)
		waitForProcessState(t, manager, StateStopped, 0)
	}
}

func TestStopSafetyBackgroundIsBounded(t *testing.T) {
	p := newStubbornProcess(902)
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- manager.Stop(context.Background()) }()
	select {
	case err := <-result:
		if err != nil || manager.Status().Running || !p.Killed() {
			t.Errorf("后台停止必须升级终止并确认退出: err=%v state=%#v", err, manager.Status())
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Stop(context.Background()) 无界等待")
	}
	p.Exit(0, nil)
	// 即使红测试失败，也释放模拟进程，避免测试自身留下阻塞。
	waitForProcessState(t, manager, StateStopped, 0)
}

func TestStopSafetyKillFailureRetainsProcessAndCanRetry(t *testing.T) {
	sentinel := errors.New("fixture kill denied")
	p := &stopSafetyProcess{scriptedProcess: newScriptedProcess(903), killErr: sentinel}
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := manager.Stop(ctx)
	cancel()
	if !errors.Is(err, sentinel) || !manager.Status().Running || manager.Status().PID != p.PID() {
		t.Errorf("终止错误必须传播并保留进程: err=%v state=%#v", err, manager.Status())
	}
	p.mu.Lock()
	p.killErr = nil
	p.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	err = manager.Stop(ctx)
	cancel()
	if err != nil || manager.Status().Running {
		t.Errorf("终止失败后必须允许重试: err=%v state=%#v", err, manager.Status())
	}
	p.Exit(0, nil)
}

func TestStopSafetyRequiresTreeExitConfirmation(t *testing.T) {
	p := &stopSafetyProcess{scriptedProcess: newScriptedProcess(904), treeErr: ErrTerminationTimeout}
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := manager.Stop(ctx)
	cancel()
	if !errors.Is(err, ErrTerminationTimeout) || !manager.Status().Running {
		t.Errorf("入口退出不等于整棵树退出: err=%v state=%#v", err, manager.Status())
	}
	p.mu.Lock()
	p.treeErr = nil
	p.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	err = manager.Stop(ctx)
	cancel()
	if err != nil || manager.Status().Running {
		t.Errorf("确认进程树退出后重试应成功: err=%v state=%#v", err, manager.Status())
	}
}

func TestStopSafetyBackgroundTimeoutRetainsState(t *testing.T) {
	p := &stopSafetyProcess{scriptedProcess: newScriptedProcess(905), refuseExit: true}
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- manager.Stop(context.Background()) }()
	select {
	case err := <-result:
		if !errors.Is(err, ErrTerminationTimeout) || !manager.Status().Running {
			t.Errorf("终止信号成功但未退出必须保留实例: err=%v state=%#v", err, manager.Status())
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("停止未退出进程超出时间上限")
	}
	p.Exit(0, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Errorf("进程退出后重试失败: %v", err)
	}
}

func TestStopSafetyNaturalExitStillRequiresTreeConfirmation(t *testing.T) {
	p := &stopSafetyProcess{scriptedProcess: newScriptedProcess(906), treeErr: ErrTerminationTimeout}
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	p.Exit(0, nil)
	waitForProcessState(t, manager, StateStopping, p.PID())
	if !manager.Status().Running {
		t.Error("自然退出但后代未确认结束，不得发布 stopped")
	}
	p.mu.Lock()
	p.treeErr = nil
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStopSafetyCanceledWhileWaitingForOperationLock(t *testing.T) {
	p := newScriptedProcess(907)
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: p}}}, newProcessTestClock(true))
	if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
		t.Fatal(err)
	}
	manager.opMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	result := make(chan error, 1)
	go func() { result <- manager.Stop(ctx) }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) || p.Interrupted() {
			t.Errorf("排队取消不得发出信号: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("操作锁等待不可取消")
	}
	cancel()
	manager.opMu.Unlock()
	p.Exit(0, nil)
}

type stopSafetyProcess struct {
	*scriptedProcess
	mu         sync.Mutex
	killErr    error
	treeErr    error
	refuseExit bool
}

func (*stopSafetyProcess) Interrupt() error { return nil }

func (p *stopSafetyProcess) KillTree() error {
	p.mu.Lock()
	err := p.killErr
	refuseExit := p.refuseExit
	p.mu.Unlock()
	if err == nil && !refuseExit {
		p.Exit(137, errors.New("fixture terminated"))
	}
	return err
}

func (p *stopSafetyProcess) WaitTree(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.treeErr
}
