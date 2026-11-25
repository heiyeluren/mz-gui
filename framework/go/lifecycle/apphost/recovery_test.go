package apphost

import (
	"context"
	"errors"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/sidecargroup"
	"testing"
)

type recoveringRuntime struct {
	fakeSidecarRuntime
	restarts   int
	restartErr error
	id         string
}

func (r *recoveringRuntime) Restart(_ context.Context, id string) error {
	r.restarts++
	r.id = id
	return r.restartErr
}

func TestHostRecoveryPreservesLifecycleAndRejectsShutdown(t *testing.T) {
	r := &recoveringRuntime{fakeSidecarRuntime: fakeSidecarRuntime{snapshot: sidecargroup.Snapshot{Ready: true}}}
	host, err := New(Options{Sidecars: r})
	if err != nil {
		t.Fatal(err)
	}
	r.startErr = errors.New("initial failure")
	if err = host.Start(context.Background()); err == nil {
		t.Fatal("应记录首次启动失败")
	}
	if err = host.RestartSidecar(context.Background(), "daemon"); err != nil {
		t.Fatal(err)
	}
	if !host.Snapshot().Ready || r.id != "daemon" {
		t.Fatal("未恢复宿主状态")
	}
	if err = host.HandleSystemEvent(context.Background(), SystemWillSleep); err != nil {
		t.Fatal(err)
	}
	if err = host.RestartSidecar(context.Background(), "daemon"); err == nil {
		t.Fatal("休眠时不应重启")
	}
	if err = host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = host.RestartSidecar(context.Background(), "daemon"); err == nil || r.restarts != 1 {
		t.Fatal("退出后不能重启")
	}
}
