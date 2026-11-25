package sidecargroup

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/heiyeluren/mz-gui/framework/go/lifecycle"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
)

func TestGroupStartsDependenciesAndStopsInReverseOrder(t *testing.T) {
	recorder := &operationRecorder{}
	managers := map[string]*fakeManager{
		"daemon":       readyManager("daemon", recorder),
		"browser-host": readyManager("browser-host", recorder),
		"helper":       readyManager("helper", recorder),
	}
	group, err := New([]Declaration{
		{ID: "helper", Process: managedprocess.Spec{Executable: "/framework/helper"}, Requires: []string{"daemon"}},
		{ID: "daemon", Process: managedprocess.Spec{Executable: "/framework/daemon"}},
		{ID: "browser-host", Process: managedprocess.Spec{Executable: "/framework/browser-host"}, Requires: []string{"daemon"}},
	}, Options{
		MaxParallelStarts: 2,
		Factory:           func(declaration Declaration) ProcessManager { return managers[declaration.ID] },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := group.Snapshot()
	if !snapshot.Running || !snapshot.Ready || snapshot.Degraded || len(snapshot.Sidecars) != 3 {
		t.Fatalf("Sidecar Group 聚合健康异常: %#v", snapshot)
	}
	if err := group.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	operations := recorder.snapshot()
	if indexOf(operations, "stop:daemon") < indexOf(operations, "stop:helper") ||
		indexOf(operations, "stop:daemon") < indexOf(operations, "stop:browser-host") {
		t.Fatalf("依赖进程必须最后停止: %v", operations)
	}
}

func TestGroupAllowsOptionalFailureAndReportsDegradedHealth(t *testing.T) {
	unavailable := errors.New("optional unavailable")
	recorder := &operationRecorder{}
	managers := map[string]*fakeManager{
		"required": readyManager("required", recorder),
		"optional": {
			id: "optional", recorder: recorder, startErr: unavailable,
			snapshot: managedprocess.Snapshot{ProcessID: "optional", State: managedprocess.StateStopped},
		},
	}
	group, err := New([]Declaration{
		{ID: "required", Process: managedprocess.Spec{Executable: "/framework/required"}},
		{ID: "optional", Process: managedprocess.Spec{Executable: "/framework/optional"}, FailurePolicy: lifecycle.FailureOptional},
	}, Options{Factory: func(declaration Declaration) ProcessManager { return managers[declaration.ID] }})
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Start(context.Background()); err != nil {
		t.Fatalf("可选 Sidecar 失败不应阻断组合根: %v", err)
	}
	snapshot := group.Snapshot()
	if !snapshot.Running || !snapshot.Ready || !snapshot.Degraded {
		t.Fatalf("可选失败聚合状态错误: %#v", snapshot)
	}
	optional := sidecarOf(t, snapshot, "optional")
	if optional.Required || optional.Ready {
		t.Fatalf("可选 Sidecar 状态错误: %#v", optional)
	}
}

func TestGroupRollsBackRequiredIncompatibleProcess(t *testing.T) {
	recorder := &operationRecorder{}
	base := readyManager("base", recorder)
	incompatible := &fakeManager{
		id: "incompatible", recorder: recorder, startErr: managedprocess.ErrIncompatible,
		snapshot: managedprocess.Snapshot{ProcessID: "incompatible", State: managedprocess.StateIncompatible, Incompatible: true},
	}
	group, err := New([]Declaration{
		{ID: "base", Process: managedprocess.Spec{Executable: "/framework/base"}},
		{ID: "incompatible", Process: managedprocess.Spec{Executable: "/framework/incompatible"}, Requires: []string{"base"}},
	}, Options{Factory: func(declaration Declaration) ProcessManager {
		if declaration.ID == "base" {
			return base
		}
		return incompatible
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Start(context.Background()); !errors.Is(err, managedprocess.ErrIncompatible) {
		t.Fatalf("必需进程不兼容必须阻断启动: %v", err)
	}
	if indexOf(recorder.snapshot(), "stop:base") < 0 {
		t.Fatalf("必需进程失败后未回滚已启动依赖: %v", recorder.snapshot())
	}
	snapshot := group.Snapshot()
	if snapshot.Running || snapshot.Ready || !snapshot.Incompatible {
		t.Fatalf("不兼容聚合状态错误: %#v", snapshot)
	}
}

func TestGroupValidatesAndFreezesDeclarations(t *testing.T) {
	if _, err := New(nil, Options{}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("空声明必须被拒绝: %v", err)
	}
	if _, err := New([]Declaration{{ID: "logical", Process: managedprocess.Spec{ID: "different", Executable: "/framework/process"}}}, Options{}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("进程 ID 与逻辑 ID 不一致必须被拒绝: %v", err)
	}
	declarations := []Declaration{{
		ID: "stable", Process: managedprocess.Spec{
			Executable: "/framework/stable", Arguments: []string{"original"}, Environment: map[string]string{"MODE": "original"},
		},
	}}
	manager := readyManager("stable", &operationRecorder{})
	var received Declaration
	group, err := New(declarations, Options{Factory: func(declaration Declaration) ProcessManager {
		received = declaration
		return manager
	}})
	if err != nil {
		t.Fatal(err)
	}
	declarations[0].ID = "changed"
	declarations[0].Process.Arguments[0] = "changed"
	declarations[0].Process.Environment["MODE"] = "changed"
	if received.Process.ID != "stable" || received.Process.Arguments[0] != "original" || received.Process.Environment["MODE"] != "original" {
		t.Fatalf("构造时未规范化并冻结进程声明: %#v", received)
	}
	if err := group.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.received.ID != "stable" || manager.received.Arguments[0] != "original" || manager.received.Environment["MODE"] != "original" {
		t.Fatalf("运行时声明被外部修改: %#v", manager.received)
	}
}

func TestProcessComponentCoversHealthAndCleanupBoundaries(t *testing.T) {
	recorder := &operationRecorder{}
	tests := []struct {
		name     string
		snapshot managedprocess.Snapshot
		want     error
	}{
		{name: "ready", snapshot: managedprocess.Snapshot{State: managedprocess.StateRunning, Running: true, Ready: true}},
		{name: "degraded_ready", snapshot: managedprocess.Snapshot{State: managedprocess.StateDegraded, Running: true, Ready: true, Degraded: true}},
		{name: "incompatible", snapshot: managedprocess.Snapshot{State: managedprocess.StateIncompatible, Incompatible: true}, want: managedprocess.ErrIncompatible},
		{name: "crash_loop", snapshot: managedprocess.Snapshot{State: managedprocess.StateCrashLoop, CrashLoop: true}, want: managedprocess.ErrCrashLoop},
		{name: "not_ready", snapshot: managedprocess.Snapshot{State: managedprocess.StateStarting, Running: true}, want: ErrNotReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			component := &processComponent{manager: &fakeManager{id: test.name, recorder: recorder, snapshot: test.snapshot}}
			err := component.Ready(context.Background())
			if !errors.Is(err, test.want) || (test.want == nil && err != nil) {
				t.Fatalf("健康映射异常: want=%v got=%v", test.want, err)
			}
		})
	}

	startFailure := errors.New("start failed")
	cleanupFailure := errors.New("cleanup failed")
	manager := &fakeManager{
		id: "cleanup", recorder: recorder, startErr: startFailure, stopErr: cleanupFailure,
		snapshot: managedprocess.Snapshot{State: managedprocess.StateBackoff, Running: true},
	}
	component := &processComponent{
		declaration: Declaration{ID: "cleanup", Timeouts: lifecycle.Timeouts{Stop: 10}}, manager: manager,
	}
	if err := component.Start(context.Background()); !errors.Is(err, startFailure) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("启动失败后的清理错误不得丢失: %v", err)
	}
	manager.stopErr = managedprocess.ErrNotRunning
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("已停止进程应按幂等成功处理: %v", err)
	}
	manager.stopErr = managedprocess.ErrDisabled
	if err := component.Stop(context.Background()); err != nil {
		t.Fatalf("已禁用进程应按幂等成功处理: %v", err)
	}
}

func TestGroupNilBoundariesAndDefaultFactory(t *testing.T) {
	var group *Group
	if err := group.Start(context.Background()); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("nil Group Start 必须返回参数错误: %v", err)
	}
	if err := group.Stop(context.Background()); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("nil Group Stop 必须返回参数错误: %v", err)
	}
	if snapshot := group.Snapshot(); len(snapshot.Sidecars) != 0 {
		t.Fatalf("nil Group 快照必须为空: %#v", snapshot)
	}
	if _, err := New([]Declaration{{ID: "nil-manager", Process: managedprocess.Spec{Executable: "/framework/process"}}}, Options{
		Factory: func(Declaration) ProcessManager { return (*fakeManager)(nil) },
	}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("nil 进程管理器必须被拒绝: %v", err)
	}
	if _, err := New([]Declaration{{ID: "panic-manager", Process: managedprocess.Spec{Executable: "/framework/process"}}}, Options{
		Factory: func(Declaration) ProcessManager { panic("factory panic") },
	}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("进程工厂 panic 必须被错误边界隔离: %v", err)
	}
	if _, err := New([]Declaration{{ID: "conflict", Process: managedprocess.Spec{Executable: "/framework/process"}}}, Options{
		MaxParallelStarts: 2, Lifecycle: lifecycle.Options{MaxParallelStarts: 3},
		Factory: func(Declaration) ProcessManager { return readyManager("conflict", &operationRecorder{}) },
	}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("冲突并行度配置必须被拒绝: %v", err)
	}
	defaultGroup, err := New([]Declaration{{ID: "default", Process: managedprocess.Spec{Executable: "/framework/process"}}}, Options{})
	if err != nil || len(defaultGroup.Snapshot().Sidecars) != 1 {
		t.Fatalf("默认工厂必须创建受管进程管理器: group=%#v err=%v", defaultGroup, err)
	}
}

type fakeManager struct {
	mu       sync.Mutex
	id       string
	recorder *operationRecorder
	received managedprocess.Spec
	snapshot managedprocess.Snapshot
	startErr error
	stopErr  error
}

func readyManager(id string, recorder *operationRecorder) *fakeManager {
	return &fakeManager{id: id, recorder: recorder, snapshot: managedprocess.Snapshot{
		ProcessID: id, State: managedprocess.StateRunning, Running: true, Ready: true,
	}}
}

func (manager *fakeManager) Start(_ context.Context, spec managedprocess.Spec) (managedprocess.Snapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.received = cloneTestSpec(spec)
	manager.recorder.add("start:" + manager.id)
	return manager.snapshot, manager.startErr
}

func (manager *fakeManager) Status() managedprocess.Snapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.snapshot
}

func (manager *fakeManager) Stop(context.Context) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.recorder.add("stop:" + manager.id)
	manager.snapshot.State = managedprocess.StateStopped
	manager.snapshot.Running = false
	manager.snapshot.Ready = false
	return manager.stopErr
}

type operationRecorder struct {
	mu         sync.Mutex
	operations []string
}

func (recorder *operationRecorder) add(operation string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.operations = append(recorder.operations, operation)
}

func (recorder *operationRecorder) snapshot() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]string(nil), recorder.operations...)
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func sidecarOf(t *testing.T, snapshot Snapshot, id string) SidecarSnapshot {
	t.Helper()
	for _, sidecar := range snapshot.Sidecars {
		if sidecar.ID == id {
			return sidecar
		}
	}
	t.Fatalf("快照缺少 Sidecar %s: %#v", id, snapshot)
	return SidecarSnapshot{}
}

func cloneTestSpec(spec managedprocess.Spec) managedprocess.Spec {
	spec.Arguments = append([]string(nil), spec.Arguments...)
	return spec
}
