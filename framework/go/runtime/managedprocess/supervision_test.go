package managedprocess

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

func TestManagerEntersCrashLoopAndSupportsManualRecovery(t *testing.T) {
	clock := newProcessTestClock(true)
	first := newScriptedProcess(101)
	second := newScriptedProcess(102)
	third := newScriptedProcess(103)
	fourth := newScriptedProcess(104)
	launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}, {process: third}, {process: fourth}}}
	observer := &processRecordingObserver{}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(observer, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: 4 * time.Second,
		Multiplier: 2, Window: time.Minute, MaxRestarts: 2, StableAfter: 10 * time.Second,
	})

	started, err := manager.Start(context.Background(), spec)
	if err != nil || started.State != StateRunning || started.PID != 101 {
		t.Fatalf("首次启动异常: snapshot=%#v err=%v", started, err)
	}
	first.Exit(11, errors.New("first crash"))
	waitForProcessState(t, manager, StateRunning, 102)
	second.Exit(12, errors.New("second crash"))
	waitForProcessState(t, manager, StateRunning, 103)
	third.Exit(13, errors.New("third crash"))
	waitForProcessState(t, manager, StateCrashLoop, 0)

	crashed := manager.Status()
	if !crashed.CrashLoop || crashed.RestartCount != 2 || crashed.ConsecutiveFailures != 3 || crashed.ExitCode != 13 {
		t.Fatalf("崩溃循环快照异常: %#v", crashed)
	}
	if _, err := manager.Start(context.Background(), spec); !errors.Is(err, ErrCrashLoop) {
		t.Fatalf("崩溃循环中普通 Start 必须被拒绝: %v", err)
	}
	recovered, err := manager.Retry(context.Background())
	if err != nil || recovered.State != StateRunning || recovered.PID != 104 || recovered.CrashLoop ||
		recovered.ConsecutiveFailures != 0 || recovered.RestartCount != 2 || len(recovered.RestartHistory) != 2 || recovered.Attempt != 4 {
		t.Fatalf("人工恢复异常: snapshot=%#v err=%v", recovered, err)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForProcessState(t, manager, StateDisabled, 0)
	if launcher.CallCount() != 4 {
		t.Fatalf("禁用后不得继续重启: calls=%d", launcher.CallCount())
	}
	observer.AssertOrderedAndContains(t, EventCrashLoop, EventRecovered, EventDisabled)
}

func TestManagerAppliesRestartPolicies(t *testing.T) {
	tests := []struct {
		name       string
		policy     RestartPolicy
		exitCode   int
		wantCalls  int
		wantState  State
		secondExit bool
	}{
		{name: "never 不重启异常退出", policy: RestartNever, exitCode: 8, wantCalls: 1, wantState: StateStopped},
		{name: "on-failure 不重启正常退出", policy: RestartOnFailure, exitCode: 0, wantCalls: 1, wantState: StateStopped},
		{name: "always 重启正常退出", policy: RestartAlways, exitCode: 0, wantCalls: 2, wantState: StateRunning, secondExit: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newProcessTestClock(true)
			first := newScriptedProcess(201)
			second := newScriptedProcess(202)
			launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}
			manager := newTestProcessManager(launcher, clock)
			spec := supervisedSpec(nil, RestartConfig{
				Policy: test.policy, InitialBackoff: time.Second, MaxBackoff: time.Second,
				Multiplier: 1, Window: time.Minute, MaxRestarts: 1, StableAfter: time.Minute,
			})
			if _, err := manager.Start(context.Background(), spec); err != nil {
				t.Fatal(err)
			}
			first.Exit(test.exitCode, nil)
			waitForProcessState(t, manager, test.wantState, map[bool]int{true: 202, false: 0}[test.wantState == StateRunning])
			if launcher.CallCount() != test.wantCalls {
				t.Fatalf("重启策略调用次数异常: got=%d want=%d", launcher.CallCount(), test.wantCalls)
			}
			if test.secondExit {
				if err := manager.Disable(context.Background()); err != nil {
					t.Fatal(err)
				}
				waitForProcessState(t, manager, StateDisabled, 0)
			}
		})
	}
}

func TestManagerCancelsPendingBackoffOnStop(t *testing.T) {
	clock := newProcessTestClock(false)
	first := newScriptedProcess(301)
	second := newScriptedProcess(302)
	launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(nil, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Minute, MaxBackoff: time.Minute,
		Multiplier: 1, Window: time.Hour, MaxRestarts: 3, StableAfter: time.Hour,
	})
	if _, err := manager.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	first.Exit(9, errors.New("crash before backoff"))
	waitForProcessState(t, manager, StateBackoff, 0)
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForProcessState(t, manager, StateStopped, 0)
	clock.Advance(time.Hour)
	time.Sleep(10 * time.Millisecond)
	if launcher.CallCount() != 1 {
		t.Fatalf("停止后 backoff 不得再次启动: calls=%d", launcher.CallCount())
	}
}

func TestManagerSupervisionOutlivesStartRequestContext(t *testing.T) {
	type contextKey string
	const traceKey contextKey = "trace"
	clock := newProcessTestClock(true)
	first := newScriptedProcess(321)
	second := newScriptedProcess(322)
	launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}
	var observerMu sync.Mutex
	var restartContextErr error
	var restartTrace string
	observer := ObserverFunc(func(ctx context.Context, event Event) {
		if event.ReasonCode != ReasonRestartScheduled {
			return
		}
		observerMu.Lock()
		defer observerMu.Unlock()
		restartContextErr = ctx.Err()
		restartTrace, _ = ctx.Value(traceKey).(string)
	})
	manager := newTestProcessManager(launcher, clock)
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), traceKey, "trace-53"))
	spec := supervisedSpec(observer, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: time.Second,
		Multiplier: 1, Window: time.Minute, MaxRestarts: 1, StableAfter: time.Minute,
	})
	if _, err := manager.Start(parent, spec); err != nil {
		t.Fatal(err)
	}
	cancel()
	first.Exit(8, errors.New("request ended"))
	waitForProcessState(t, manager, StateRunning, 322)
	observerMu.Lock()
	if restartContextErr != nil || restartTrace != "trace-53" {
		t.Fatalf("监管循环必须保留请求值但不继承取消状态: err=%v trace=%q", restartContextErr, restartTrace)
	}
	observerMu.Unlock()
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerFreezesDeclarativeSpecAcrossRestarts(t *testing.T) {
	clock := newProcessTestClock(true)
	first := newScriptedProcess(331)
	second := newScriptedProcess(332)
	launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(nil, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: time.Second,
		Multiplier: 1, Window: time.Minute, MaxRestarts: 1, StableAfter: time.Minute,
	})
	spec.Arguments = []string{"--mode=original"}
	spec.Environment = map[string]string{"MZ_GUI_MODE": "original"}
	if _, err := manager.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	spec.Arguments[0] = "--mode=mutated"
	spec.Environment["MZ_GUI_MODE"] = "mutated"
	first.Exit(8, errors.New("restart with frozen spec"))
	waitForProcessState(t, manager, StateRunning, 332)
	launched := launcher.Specs()
	if len(launched) != 2 || launched[1].Arguments[0] != "--mode=original" || launched[1].Environment["MZ_GUI_MODE"] != "original" {
		t.Fatalf("自动重启必须使用构造时冻结的 Spec: %#v", launched)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStopsProcessLaunchedWhileStopRacesWithRestart(t *testing.T) {
	clock := newProcessTestClock(true)
	first := newScriptedProcess(351)
	second := newScriptedProcess(352)
	launcher := &gatedRestartLauncher{
		first: first, second: second, restartStarted: make(chan struct{}), releaseRestart: make(chan struct{}),
	}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(nil, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: time.Second,
		Multiplier: 1, Window: time.Minute, MaxRestarts: 2, StableAfter: time.Minute,
	})
	if _, err := manager.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	first.Exit(7, errors.New("restart race"))
	select {
	case <-launcher.restartStarted:
	case <-time.After(time.Second):
		t.Fatal("重启启动器未进入受控阻塞点")
	}
	stopResult := make(chan error, 1)
	go func() { stopResult <- manager.Stop(context.Background()) }()
	waitForProcessState(t, manager, StateStopping, 0)
	close(launcher.releaseRestart)
	select {
	case err := <-stopResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("停止请求未终止并发启动的替代进程")
	}
	waitForProcessState(t, manager, StateStopped, 0)
	if !second.Interrupted() {
		t.Fatal("Stop 与重启竞争时，新进程必须在进入 Wait 前收到终止信号")
	}
}

func TestManagerCountsRestartLaunchFailuresAndStableRunsResetWindow(t *testing.T) {
	t.Run("重启启动失败进入 crash-loop", func(t *testing.T) {
		clock := newProcessTestClock(true)
		first := newScriptedProcess(361)
		observer := &processRecordingObserver{}
		launcher := &scriptedLauncher{results: []launchResult{
			{process: first}, {err: errors.New("restart launch one")}, {err: errors.New("restart launch two")},
		}}
		manager := newTestProcessManager(launcher, clock)
		spec := supervisedSpec(observer, RestartConfig{
			Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: 2 * time.Second,
			Multiplier: 2, Window: time.Minute, MaxRestarts: 2, StableAfter: time.Minute,
		})
		if _, err := manager.Start(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		first.Exit(9, errors.New("initial crash"))
		waitForProcessState(t, manager, StateCrashLoop, 0)
		if launcher.CallCount() != 3 || manager.Status().ConsecutiveFailures != 3 {
			t.Fatalf("重启启动失败计数异常: calls=%d snapshot=%#v", launcher.CallCount(), manager.Status())
		}
		observer.AssertContainsReason(t, ReasonLaunchFailed)
	})

	t.Run("稳定运行重置窗口", func(t *testing.T) {
		clock := newProcessTestClock(true)
		first := newScriptedProcess(371)
		second := newScriptedProcess(372)
		third := newScriptedProcess(373)
		launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}, {process: third}}}
		manager := newTestProcessManager(launcher, clock)
		spec := supervisedSpec(nil, RestartConfig{
			Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: time.Second,
			Multiplier: 1, Window: 10 * time.Minute, MaxRestarts: 1, StableAfter: time.Minute,
		})
		if _, err := manager.Start(context.Background(), spec); err != nil {
			t.Fatal(err)
		}
		clock.Advance(2 * time.Minute)
		first.Exit(8, errors.New("first stable exit"))
		waitForProcessState(t, manager, StateRunning, 372)
		clock.Advance(2 * time.Minute)
		second.Exit(8, errors.New("second stable exit"))
		waitForProcessState(t, manager, StateRunning, 373)
		snapshot := manager.Status()
		if snapshot.CrashLoop || snapshot.RestartCount != 2 || snapshot.ConsecutiveFailures != 1 {
			t.Fatalf("稳定运行未重置崩溃窗口: %#v", snapshot)
		}
		snapshot.RestartHistory[0].ReasonCode = "tampered"
		if manager.Status().RestartHistory[0].ReasonCode == "tampered" {
			t.Fatal("Status 必须返回重启历史深拷贝")
		}
		if err := manager.Disable(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestManagerValidatesRestartPolicyAndContainsCallbacks(t *testing.T) {
	clock := newProcessTestClock(true)
	process := newScriptedProcess(401)
	launcher := &scriptedLauncher{results: []launchResult{{process: process}}}
	manager := newTestProcessManager(launcher, clock)
	invalid := supervisedSpec(nil, RestartConfig{Policy: "sometimes"})
	if _, err := manager.Start(context.Background(), invalid); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("非法重启策略应被拒绝: %v", err)
	}
	spec := supervisedSpec(ObserverFunc(func(context.Context, Event) { panic("observer panic") }), RestartConfig{Policy: RestartNever})
	spec.OnExit = func(Snapshot) { panic("on-exit panic") }
	if _, err := manager.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	process.Exit(0, nil)
	waitForProcessState(t, manager, StateStopped, 0)
}

func TestBuiltinManagedProcessContractMatchesPublicConstants(t *testing.T) {
	contract, err := BuiltinContract()
	if err != nil {
		t.Fatal(err)
	}
	if len(contract.States) != 9 || len(contract.RestartPolicies) != 3 || len(contract.EventKinds) != 9 || len(contract.Reasons) != 18 {
		t.Fatalf("进程契约条目数量异常: states=%d policies=%d kinds=%d reasons=%d",
			len(contract.States), len(contract.RestartPolicies), len(contract.EventKinds), len(contract.Reasons))
	}
	for _, state := range []State{StateStopped, StateStarting, StateRunning, StateDegraded, StateBackoff, StateCrashLoop, StateIncompatible, StateStopping, StateDisabled} {
		if !contract.HasState(state) {
			t.Errorf("进程契约缺少状态 %s", state)
		}
	}
	for _, policy := range []RestartPolicy{RestartNever, RestartOnFailure, RestartAlways} {
		if !contract.HasRestartPolicy(policy) {
			t.Errorf("进程契约缺少重启策略 %s", policy)
		}
	}
	for _, phase := range []ProbePhase{ProbePhaseReadiness, ProbePhaseHeartbeat} {
		if !contract.HasProbePhase(phase) {
			t.Errorf("进程契约缺少 Probe 阶段 %s", phase)
		}
	}
	for _, status := range []ProbeStatus{ProbeStatusReady, ProbeStatusDegraded, ProbeStatusNotReady, ProbeStatusIncompatible} {
		if !contract.HasProbeStatus(status) {
			t.Errorf("进程契约缺少 Probe 状态 %s", status)
		}
	}
	for _, kind := range []EventKind{EventStarting, EventReady, EventExited, EventDegraded, EventCrashLoop, EventIncompatible, EventRecovered, EventStopped, EventDisabled} {
		if !contract.HasEventKind(kind) {
			t.Errorf("进程契约缺少事件 %s", kind)
		}
	}
	checks := []struct {
		reason string
		kind   EventKind
		state  State
	}{
		{ReasonStarting, EventStarting, StateStarting},
		{ReasonReady, EventReady, StateRunning},
		{ReasonExitedClean, EventExited, StateStopped},
		{ReasonExitedFailure, EventExited, StateStopped},
		{ReasonRestartScheduled, EventDegraded, StateBackoff},
		{ReasonLaunchFailed, EventExited, StateStopped},
		{ReasonCrashLoop, EventCrashLoop, StateCrashLoop},
		{ReasonManualRetry, EventRecovered, StateStopped},
		{ReasonStopRequested, EventDegraded, StateStopping},
		{ReasonStopped, EventStopped, StateStopped},
		{ReasonDisabled, EventDisabled, StateDisabled},
		{ReasonReadinessTimeout, EventExited, StateStopped},
		{ReasonReadinessFailed, EventExited, StateStopped},
		{ReasonHealthDegraded, EventDegraded, StateDegraded},
		{ReasonHeartbeatFailed, EventDegraded, StateDegraded},
		{ReasonHeartbeatTerminated, EventExited, StateStopped},
		{ReasonHeartbeatRecovered, EventRecovered, StateRunning},
		{ReasonIncompatible, EventIncompatible, StateIncompatible},
	}
	for _, check := range checks {
		if !contract.Allows(check.reason, check.kind, check.state) {
			t.Errorf("进程契约未登记 %s/%s/%s", check.reason, check.kind, check.state)
		}
	}
	if contract.Allows("missing", EventReady, StateRunning) || contract.Allows(ReasonReady, EventExited, StateRunning) {
		t.Fatal("进程契约不得允许未登记组合")
	}

	invalidContracts := [][]byte{
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]}],"unexpected":true}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped","stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never","never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped","process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.ready"],"target_states":["stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["running"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped","process.stopped"],"target_states":["stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped","stopped"]}]}`),
		[]byte(`{"$schema":"./v1.schema.json","schema_version":1,"release":"1.0.0","states":["stopped"],"restart_policies":["never"],"event_kinds":["process.stopped"],"reasons":[{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]},{"reason_code":"process.stopped","event_kinds":["process.stopped"],"target_states":["stopped"]}]}`),
	}
	for _, data := range invalidContracts {
		if _, err := DecodeContract(data); !errors.Is(err, ErrInvalidContract) {
			t.Errorf("非法进程契约应被拒绝: %v", err)
		}
	}
}

func TestManagerEdgeContractsAndForcedStop(t *testing.T) {
	t.Run("默认值与非法组合", func(t *testing.T) {
		defaults, err := normalizeRestart(RestartConfig{})
		if err != nil || defaults.Policy != RestartNever || defaults.MaxRestarts != defaultMaxRestarts {
			t.Fatalf("默认重启策略异常: %#v err=%v", defaults, err)
		}
		invalid := []RestartConfig{
			{Policy: "invalid"},
			{Policy: RestartAlways, Jitter: 1.1},
			{Policy: RestartAlways, Multiplier: .5},
			{Policy: RestartAlways, InitialBackoff: 2 * time.Second, MaxBackoff: time.Second},
			{Policy: RestartAlways, InitialBackoff: 2 * time.Second, MaxBackoff: 2 * time.Second, Window: time.Second},
		}
		for _, value := range invalid {
			if _, err := normalizeRestart(value); !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("非法重启组合应被拒绝: %#v err=%v", value, err)
			}
		}
		if unit := secureRandomUnit(); unit < 0 || unit >= 1 {
			t.Fatalf("随机抖动值越界: %f", unit)
		}
	})

	t.Run("调用契约", func(t *testing.T) {
		manager := newTestProcessManager(&scriptedLauncher{}, newProcessTestClock(true))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := manager.Start(ctx, supervisedSpec(nil, RestartConfig{})); !errors.Is(err, context.Canceled) {
			t.Fatalf("已取消 Start 应原样返回: %v", err)
		}
		if _, err := manager.Retry(context.Background()); !errors.Is(err, ErrNotRunning) {
			t.Fatalf("非 crash-loop 不得 Retry: %v", err)
		}
		if err := manager.Disable(context.Background()); err != nil || manager.Status().State != StateDisabled {
			t.Fatalf("未启动实例应允许直接禁用: state=%#v err=%v", manager.Status(), err)
		}
		if err := manager.Disable(context.Background()); err != nil {
			t.Fatalf("重复禁用必须幂等: %v", err)
		}
		if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); !errors.Is(err, ErrDisabled) {
			t.Fatalf("禁用状态不得启动: %v", err)
		}
		var nilManager *Manager
		if !reflect.DeepEqual(nilManager.Status(), Snapshot{}) || nilManager.Stop(context.Background()) != ErrInvalidSpec {
			t.Fatal("nil Manager 边界异常")
		}
	})

	t.Run("超时后强制停止", func(t *testing.T) {
		clock := newProcessTestClock(true)
		process := newStubbornProcess(451)
		launcher := &scriptedLauncher{results: []launchResult{{process: process}}}
		manager := newTestProcessManager(launcher, clock)
		manager.forceKillWait = 50 * time.Millisecond
		if _, err := manager.Start(context.Background(), supervisedSpec(nil, RestartConfig{})); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		if err := manager.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("强制停止应保留调用方 deadline: %v", err)
		}
		waitForProcessState(t, manager, StateStopped, 0)
		if !process.Killed() {
			t.Fatal("停止超时后必须执行 KillTree")
		}
	})
}

func supervisedSpec(observer Observer, restart RestartConfig) Spec {
	return Spec{
		ID: "framework-sidecar", Executable: testExecutablePath(), Restart: restart, Observer: observer,
	}
}

func testExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return path
}

func newTestProcessManager(launcher launcher, clock frameworkclock.Clock) *Manager {
	return &Manager{launcher: launcher, clock: clock, forceKillWait: 10 * time.Millisecond}
}

type launchResult struct {
	process process
	err     error
}

type scriptedLauncher struct {
	mu      sync.Mutex
	results []launchResult
	calls   int
	specs   []Spec
}

func (value *scriptedLauncher) Launch(spec Spec) (process, error) {
	value.mu.Lock()
	defer value.mu.Unlock()
	value.calls++
	value.specs = append(value.specs, cloneSpec(spec))
	if len(value.results) == 0 {
		return nil, errors.New("没有剩余进程脚本")
	}
	result := value.results[0]
	value.results = value.results[1:]
	return result.process, result.err
}

func (value *scriptedLauncher) Specs() []Spec {
	value.mu.Lock()
	defer value.mu.Unlock()
	result := make([]Spec, 0, len(value.specs))
	for _, spec := range value.specs {
		result = append(result, cloneSpec(spec))
	}
	return result
}

func (value *scriptedLauncher) CallCount() int {
	value.mu.Lock()
	defer value.mu.Unlock()
	return value.calls
}

type processResult struct {
	code int
	err  error
}

type scriptedProcess struct {
	pid       int
	result    chan processResult
	exit      sync.Once
	interrupt sync.Once
	stopped   chan struct{}
}

func newScriptedProcess(pid int) *scriptedProcess {
	return &scriptedProcess{pid: pid, result: make(chan processResult, 1), stopped: make(chan struct{})}
}

func (value *scriptedProcess) PID() int { return value.pid }
func (value *scriptedProcess) Wait() (int, error) {
	result := <-value.result
	return result.code, result.err
}
func (value *scriptedProcess) Interrupt() error {
	value.interrupt.Do(func() {
		close(value.stopped)
		value.Exit(0, nil)
	})
	return nil
}
func (value *scriptedProcess) KillTree() error {
	value.Exit(137, errors.New("killed"))
	return nil
}
func (value *scriptedProcess) Exit(code int, err error) {
	value.exit.Do(func() { value.result <- processResult{code: code, err: err} })
}

func (value *scriptedProcess) Interrupted() bool {
	select {
	case <-value.stopped:
		return true
	default:
		return false
	}
}

type gatedRestartLauncher struct {
	mu             sync.Mutex
	calls          int
	first          process
	second         process
	restartStarted chan struct{}
	releaseRestart chan struct{}
}

func (value *gatedRestartLauncher) Launch(Spec) (process, error) {
	value.mu.Lock()
	value.calls++
	call := value.calls
	value.mu.Unlock()
	if call == 1 {
		return value.first, nil
	}
	if call == 2 {
		close(value.restartStarted)
		<-value.releaseRestart
		return value.second, nil
	}
	return nil, errors.New("非预期启动调用")
}

type stubbornProcess struct {
	*scriptedProcess
	killed chan struct{}
	once   sync.Once
}

func newStubbornProcess(pid int) *stubbornProcess {
	return &stubbornProcess{scriptedProcess: newScriptedProcess(pid), killed: make(chan struct{})}
}

func (*stubbornProcess) Interrupt() error { return nil }
func (value *stubbornProcess) KillTree() error {
	value.once.Do(func() {
		close(value.killed)
		value.Exit(137, errors.New("forced kill"))
	})
	return nil
}
func (value *stubbornProcess) Killed() bool {
	select {
	case <-value.killed:
		return true
	default:
		return false
	}
}

type processTestTimer struct {
	due time.Time
	ch  chan time.Time
}

type processTestClock struct {
	mu          sync.Mutex
	now         time.Time
	autoAdvance bool
	timers      []processTestTimer
	tickers     []*processTestTicker
}

func newProcessTestClock(autoAdvance bool) *processTestClock {
	return &processTestClock{now: time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC), autoAdvance: autoAdvance}
}

func (value *processTestClock) Now() time.Time {
	value.mu.Lock()
	defer value.mu.Unlock()
	return value.now
}

func (value *processTestClock) After(duration time.Duration) <-chan time.Time {
	value.mu.Lock()
	defer value.mu.Unlock()
	ch := make(chan time.Time, 1)
	if value.autoAdvance {
		value.now = value.now.Add(duration)
		ch <- value.now
		close(ch)
		return ch
	}
	value.timers = append(value.timers, processTestTimer{due: value.now.Add(duration), ch: ch})
	return ch
}

func (value *processTestClock) NewTicker(interval time.Duration) (frameworkclock.Ticker, error) {
	if interval <= 0 {
		return nil, errors.New("进程监管测试 ticker 间隔无效")
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	ticker := &processTestTicker{interval: interval, due: value.now.Add(interval), values: make(chan time.Time, 16)}
	value.tickers = append(value.tickers, ticker)
	return ticker, nil
}

func (value *processTestClock) Advance(duration time.Duration) {
	value.mu.Lock()
	defer value.mu.Unlock()
	value.now = value.now.Add(duration)
	pending := value.timers[:0]
	for _, timer := range value.timers {
		if timer.due.After(value.now) {
			pending = append(pending, timer)
			continue
		}
		timer.ch <- timer.due
		close(timer.ch)
	}
	value.timers = pending
	for _, ticker := range value.tickers {
		ticker.tick(value.now)
	}
}

func (value *processTestClock) WaitForTicker(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value.mu.Lock()
		count := len(value.tickers)
		value.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待测试 ticker 注册超时")
}

func (value *processTestClock) WaitForTimer(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		value.mu.Lock()
		count := len(value.timers)
		value.mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待测试 timer 注册超时")
}

type processTestTicker struct {
	mu       sync.Mutex
	interval time.Duration
	due      time.Time
	values   chan time.Time
	stopped  bool
}

func (ticker *processTestTicker) C() <-chan time.Time { return ticker.values }
func (ticker *processTestTicker) Stop() {
	ticker.mu.Lock()
	defer ticker.mu.Unlock()
	ticker.stopped = true
}

func (ticker *processTestTicker) tick(now time.Time) {
	ticker.mu.Lock()
	defer ticker.mu.Unlock()
	if ticker.stopped || ticker.due.After(now) {
		return
	}
	select {
	case ticker.values <- ticker.due:
	default:
	}
	for !ticker.due.After(now) {
		ticker.due = ticker.due.Add(ticker.interval)
	}
}

type processRecordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (value *processRecordingObserver) OnProcessEvent(_ context.Context, event Event) {
	value.mu.Lock()
	defer value.mu.Unlock()
	value.events = append(value.events, event)
}

func (value *processRecordingObserver) AssertOrderedAndContains(t *testing.T, kinds ...EventKind) {
	t.Helper()
	value.mu.Lock()
	defer value.mu.Unlock()
	found := make(map[EventKind]bool, len(kinds))
	for index, event := range value.events {
		if event.Sequence != uint64(index+1) || event.ProcessID == "" || event.ReasonCode == "" || event.ObservedAt.IsZero() {
			t.Fatalf("进程事件不稳定: index=%d event=%#v", index, event)
		}
		found[event.Kind] = true
	}
	for _, kind := range kinds {
		if !found[kind] {
			t.Fatalf("进程事件缺少 %s: %#v", kind, value.events)
		}
	}
}

func (value *processRecordingObserver) AssertContainsReason(t *testing.T, reason string) {
	t.Helper()
	value.mu.Lock()
	defer value.mu.Unlock()
	for _, event := range value.events {
		if event.ReasonCode == reason {
			return
		}
	}
	t.Fatalf("进程事件缺少原因 %s: %#v", reason, value.events)
}

func waitForProcessState(t *testing.T, manager *Manager, state State, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := manager.Status()
		if snapshot.State == state && (pid == 0 || snapshot.PID == pid) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待进程状态超时: want=%s pid=%d got=%#v", state, pid, manager.Status())
}
