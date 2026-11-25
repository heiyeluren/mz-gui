package sidecargroup

import (
	"context"
	"errors"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

type recoveryManager struct {
	*fakeManager
	retries int
}

func (m *recoveryManager) Start(ctx context.Context, spec managedprocess.Spec) (managedprocess.Snapshot, error) {
	m.snapshot = managedprocess.Snapshot{State: managedprocess.StateRunning, Running: true, Ready: true}
	return m.fakeManager.Start(ctx, spec)
}
func (m *recoveryManager) Retry(ctx context.Context) (managedprocess.Snapshot, error) {
	m.retries++
	return m.Start(ctx, m.received)
}

func TestRestartRecoversCrashLoopAndPreservesOtherProcesses(t *testing.T) {
	r := &operationRecorder{}
	daemon := &recoveryManager{fakeManager: readyManager("daemon", r)}
	helper := &recoveryManager{fakeManager: readyManager("helper", r)}
	group, err := New([]Declaration{{ID: "daemon"}, {ID: "helper"}}, Options{Factory: func(d Declaration) ProcessManager {
		if d.ID == "daemon" {
			return daemon
		}
		return helper
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	daemon.snapshot = managedprocess.Snapshot{State: managedprocess.StateCrashLoop, CrashLoop: true}
	if err = group.Restart(context.Background(), "daemon"); err != nil {
		t.Fatal(err)
	}
	if daemon.retries != 1 || !group.Snapshot().Ready {
		t.Fatalf("未恢复: %+v", group.Snapshot())
	}
	if len(r.snapshot()) != 3 {
		t.Fatalf("不应重启其他进程: %v", r.snapshot())
	}
	daemon.stopErr = errors.New("still running")
	before := len(r.snapshot())
	if err = group.Restart(context.Background(), "daemon"); err == nil {
		t.Fatal("停止失败不应启动")
	}
	if len(r.snapshot()) != before+1 {
		t.Fatal("停止失败后仍启动了进程")
	}
	daemon.stopErr = nil
	if err = group.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	before = len(r.snapshot())
	if err = group.Restart(context.Background(), "daemon"); err == nil || len(r.snapshot()) != before {
		t.Fatal("退出后不能重启")
	}
}

func TestRestartAfterInitialFailureIsTrackedForShutdown(t *testing.T) {
	r := &operationRecorder{}
	daemon := &recoveryManager{fakeManager: readyManager("daemon", r)}
	daemon.startErr = errors.New("first start failed")
	group, err := New([]Declaration{{ID: "daemon"}}, Options{Factory: func(Declaration) ProcessManager { return daemon }})
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Start(context.Background()); err == nil {
		t.Fatal("首次应失败")
	}
	daemon.startErr = nil
	if err := group.Restart(context.Background(), "daemon"); err != nil {
		t.Fatal(err)
	}
	if !group.Snapshot().Ready {
		t.Fatal("初次失败后未恢复")
	}
	if err := group.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if daemon.Status().Running {
		t.Fatal("恢复后的进程没有被退出链路停止")
	}
}
