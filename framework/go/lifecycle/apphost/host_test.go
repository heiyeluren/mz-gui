package apphost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/core/appmanifest"
	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/sidecargroup"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/managedprocess"
	"github.com/heiyeluren/mz-gui/framework/go/testing/testkit"
)

func TestHostStartsSuspendsResumesAndStopsSidecars(t *testing.T) {
	clock := testkit.NewManualClock(time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC))
	sidecars := &fakeSidecarRuntime{snapshot: sidecargroup.Snapshot{Running: true, Ready: true}}
	windows, err := NewWindowGroup([]string{"main"}, WindowGroupOptions{Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	events := make([]Event, 0)
	host, err := New(Options{
		Sidecars: sidecars, Windows: windows, Clock: clock,
		Observer: ObserverFunc(func(_ context.Context, event Event) { events = append(events, event) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := host.ApplyWindowEvent(context.Background(), WindowEvent{
		WindowID: "main", Kind: WindowFocus, Bounds: Bounds{X: 10, Y: 20, Width: 1440, Height: 900}, DisplayID: "display-1", Scale: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.HandleSystemEvent(context.Background(), SystemWillSleep); err != nil {
		t.Fatal(err)
	}
	if snapshot := host.Snapshot(); snapshot.State != StateSuspended || !snapshot.Suspended || snapshot.Ready {
		t.Fatalf("休眠状态异常: %#v", snapshot)
	}
	if err := host.HandleSystemEvent(context.Background(), SystemDidWake); err != nil {
		t.Fatal(err)
	}
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := sidecars.operations, []string{"start", "suspend", "resume", "stop"}; !sameStrings(got, want) {
		t.Fatalf("sidecar 操作顺序错误: got=%v want=%v", got, want)
	}
	snapshot := host.Snapshot()
	if snapshot.State != StateStopped || snapshot.Running || snapshot.Ready || len(snapshot.Windows.Windows) != 1 || snapshot.Windows.Windows[0].State != WindowFocused {
		t.Fatalf("组合根快照异常: %#v", snapshot)
	}
	if len(events) < 5 || events[0].Sequence != 1 || events[len(events)-1].ReasonCode != "apphost.stopped" {
		t.Fatalf("事件序列异常: %#v", events)
	}
}

func TestHostDegradesWhenSidecarStartOrResumeFails(t *testing.T) {
	startErr := errors.New("start failed")
	sidecars := &fakeSidecarRuntime{startErr: startErr, snapshot: sidecargroup.Snapshot{Degraded: true}}
	host, err := New(Options{Sidecars: sidecars})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); !errors.Is(err, startErr) {
		t.Fatalf("启动错误未透传: %v", err)
	}
	if snapshot := host.Snapshot(); snapshot.State != StateDegraded || !snapshot.Degraded {
		t.Fatalf("启动失败必须降级: %#v", snapshot)
	}

	resumeErr := errors.New("resume failed")
	sidecars = &fakeSidecarRuntime{resumeErr: resumeErr, snapshot: sidecargroup.Snapshot{Running: true, Ready: true}}
	host, err = New(Options{Sidecars: sidecars})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.HandleSystemEvent(context.Background(), SystemDidWake); !errors.Is(err, resumeErr) {
		t.Fatalf("恢复错误未透传: %v", err)
	}
	if snapshot := host.Snapshot(); snapshot.State != StateDegraded || !snapshot.Degraded {
		t.Fatalf("恢复失败必须降级: %#v", snapshot)
	}
}

func TestWindowGroupValidatesAndFreezesState(t *testing.T) {
	group, err := NewWindowGroup([]string{"main", "settings"}, WindowGroupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWindowGroup([]string{"main", "main"}, WindowGroupOptions{}); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("重复窗口 ID 必须拒绝: %v", err)
	}
	if _, err := group.Apply(WindowEvent{WindowID: "missing", Kind: WindowShow}); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("未知窗口必须拒绝: %v", err)
	}
	if _, err := group.Apply(WindowEvent{WindowID: "main", Kind: WindowShow, Bounds: Bounds{Width: 800, Height: 600}}); err != nil {
		t.Fatal(err)
	}
	if _, err := group.Apply(WindowEvent{WindowID: "main", Kind: WindowMinimize}); err != nil {
		t.Fatal(err)
	}
	snapshot := group.Snapshot()
	snapshot.Windows[0].State = WindowClosed
	again := group.Snapshot()
	if again.Windows[0].State == WindowClosed {
		t.Fatalf("窗口快照不能被外部修改: %#v", again)
	}
}

func TestSiblingSidecarDeclarationsUseManifestProcesses(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	processes := appmanifest.Processes{Agentd: "sample-agentd", BrowserHost: "sample-browser-host", Helper: "sample-helper"}
	createExecutable(t, desktop)
	for _, name := range []string{processes.Agentd, processes.BrowserHost, processes.Helper} {
		createExecutable(t, filepath.Join(root, name))
	}
	declarations, err := SiblingSidecarDeclarations(SiblingSidecarOptions{
		DesktopExecutable: desktop, GOOS: "darwin", Processes: processes,
		Restart: managedprocess.RestartConfig{Policy: managedprocess.RestartOnFailure},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations) != 3 || declarations[0].ID != processes.Agentd || declarations[0].FailurePolicy == "optional" {
		t.Fatalf("agentd 声明异常: %#v", declarations)
	}
	for _, declaration := range declarations[1:] {
		if len(declaration.Requires) != 1 || declaration.Requires[0] != processes.Agentd {
			t.Fatalf("非 agentd sidecar 必须依赖 agentd: %#v", declaration)
		}
	}
	if _, err := SiblingSidecarDeclarations(SiblingSidecarOptions{DesktopExecutable: "relative", Processes: processes}); !errors.Is(err, ErrInvalidHost) {
		t.Fatalf("相对 desktop 路径必须拒绝: %v", err)
	}
}

func TestSiblingSidecarDeclarationsInjectProcessEnvironmentProbeAndObserver(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop")
	processes := appmanifest.Processes{Agentd: "sample-agentd", BrowserHost: "sample-browser-host"}
	createExecutable(t, desktop)
	createExecutable(t, filepath.Join(root, processes.Agentd))
	createExecutable(t, filepath.Join(root, processes.BrowserHost))
	observer := managedprocess.ObserverFunc(func(context.Context, managedprocess.Event) {})
	probe := managedprocess.ProbeFunc(func(context.Context, managedprocess.ProbeRequest) (managedprocess.ProbeResult, error) {
		return managedprocess.ProbeResult{Status: managedprocess.ProbeStatusReady, ReasonCode: managedprocess.ReasonReady}, nil
	})
	env := map[string]string{"MZ_GUI_ENV": "test"}

	declarations, err := SiblingSidecarDeclarations(SiblingSidecarOptions{
		DesktopExecutable: desktop, GOOS: "darwin", Processes: processes, ProcessObserver: observer,
		ProcessEnv: func(string) map[string]string { return env },
		ProcessProbe: func(processID string) managedprocess.ProbeConfig {
			if processID != processes.Agentd {
				return managedprocess.ProbeConfig{}
			}
			return managedprocess.ProbeConfig{Probe: probe}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env["MZ_GUI_ENV"] = "production"

	for _, declaration := range declarations {
		if declaration.Process.Environment["MZ_GUI_ENV"] != "test" {
			t.Fatalf("%s 环境变量必须冻结副本: %#v", declaration.ID, declaration.Process.Environment)
		}
		if declaration.Process.Observer == nil {
			t.Fatalf("%s 缺少进程事件观察器", declaration.ID)
		}
		if declaration.ID == processes.Agentd && declaration.Process.Probe.Probe == nil {
			t.Fatal("agentd 必须注入 readiness probe")
		}
		if declaration.ID == processes.BrowserHost && declaration.Process.Probe.Probe != nil {
			t.Fatal("未声明 probe 的 sidecar 不应被隐式注入")
		}
	}
}

func TestSiblingSidecarDeclarationsUseWindowsSuffix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 本机文件权限语义由平台测试矩阵覆盖")
	}
	root := t.TempDir()
	desktop := filepath.Join(root, "sample-desktop.exe")
	processes := appmanifest.Processes{Agentd: "sample-agentd"}
	createExecutable(t, desktop)
	createExecutable(t, filepath.Join(root, "sample-agentd.exe"))
	declarations, err := SiblingSidecarDeclarations(SiblingSidecarOptions{DesktopExecutable: desktop, GOOS: "windows", Processes: processes})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(declarations[0].Process.Executable) != "sample-agentd.exe" {
		t.Fatalf("Windows sidecar 后缀错误: %#v", declarations[0])
	}
}

type fakeSidecarRuntime struct {
	operations []string
	snapshot   sidecargroup.Snapshot
	startErr   error
	stopErr    error
	suspendErr error
	resumeErr  error
}

func (runtime *fakeSidecarRuntime) Start(context.Context) error {
	runtime.operations = append(runtime.operations, "start")
	return runtime.startErr
}

func (runtime *fakeSidecarRuntime) Stop(context.Context) error {
	runtime.operations = append(runtime.operations, "stop")
	return runtime.stopErr
}

func (runtime *fakeSidecarRuntime) Suspend(context.Context) error {
	runtime.operations = append(runtime.operations, "suspend")
	return runtime.suspendErr
}

func (runtime *fakeSidecarRuntime) Resume(context.Context) error {
	runtime.operations = append(runtime.operations, "resume")
	return runtime.resumeErr
}

func (runtime *fakeSidecarRuntime) Snapshot() sidecargroup.Snapshot {
	return runtime.snapshot
}

func createExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
