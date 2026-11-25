package managedprocess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManagerWaitsForReadinessBeforeReportingRunning(t *testing.T) {
	clock := newProcessTestClock(true)
	child := newScriptedProcess(501)
	probe := newControlledProbe()
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: child}}}, clock)
	spec := supervisedSpec(nil, RestartConfig{Policy: RestartNever})
	spec.Probe = testProbeConfig(probe)

	started := make(chan struct {
		snapshot Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := manager.Start(context.Background(), spec)
		started <- struct {
			snapshot Snapshot
			err      error
		}{snapshot: snapshot, err: err}
	}()

	request := probe.NextRequest(t)
	if request.Phase != ProbePhaseReadiness || request.PID != 501 || request.Attempt != 1 {
		t.Fatalf("readiness 请求异常: %#v", request)
	}
	waitForProcessState(t, manager, StateStarting, 501)
	if snapshot := manager.Status(); snapshot.Running || snapshot.Ready {
		t.Fatalf("readiness 通过前不得报告运行: %#v", snapshot)
	}
	probe.Respond(ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.endpoint_pending"}, nil)
	request = probe.NextRequest(t)
	if request.Phase != ProbePhaseReadiness {
		t.Fatalf("readiness 重试阶段异常: %#v", request)
	}
	probe.Respond(readyProbeResult("1.4.0", "agentd-attempt-1"), nil)

	select {
	case result := <-started:
		if result.err != nil || result.snapshot.State != StateRunning || !result.snapshot.Ready || !result.snapshot.Running {
			t.Fatalf("readiness 完成结果异常: snapshot=%#v err=%v", result.snapshot, result.err)
		}
		assertProbeMetadata(t, result.snapshot, "1.4.0", "agentd-attempt-1")
		result.snapshot.Capabilities[0] = "tampered"
		if manager.Status().Capabilities[0] == "tampered" {
			t.Fatal("Status 必须返回 capability 深拷贝")
		}
	case <-time.After(time.Second):
		t.Fatal("Start 未在 readiness 通过后返回")
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerStopsIncompatibleProcessWithoutRestart(t *testing.T) {
	clock := newProcessTestClock(false)
	child := newScriptedProcess(511)
	replacement := newScriptedProcess(512)
	probe := newControlledProbe()
	launcher := &scriptedLauncher{results: []launchResult{{process: child}, {process: replacement}}}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(nil, RestartConfig{
		Policy: RestartAlways, InitialBackoff: time.Second, MaxBackoff: time.Second,
		Multiplier: 1, Window: time.Minute, MaxRestarts: 3, StableAfter: time.Minute,
	})
	spec.Probe = testProbeConfig(probe)

	result := make(chan error, 1)
	go func() {
		_, err := manager.Start(context.Background(), spec)
		result <- err
	}()
	probe.NextRequest(t)
	probe.Respond(ProbeResult{
		Status: ProbeStatusIncompatible, ReasonCode: "process.protocol_incompatible",
		ProcessVersion: "2.0.0", ProtocolMajor: 2,
	}, nil)

	select {
	case err := <-result:
		if !errors.Is(err, ErrIncompatible) {
			t.Fatalf("不兼容启动错误异常: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("不兼容启动未返回")
	}
	waitForProcessState(t, manager, StateIncompatible, 0)
	if !child.Interrupted() || launcher.CallCount() != 1 {
		t.Fatalf("不兼容进程必须停止且不得重启: interrupted=%v calls=%d", child.Interrupted(), launcher.CallCount())
	}
	if _, err := manager.Start(context.Background(), spec); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("不兼容状态普通 Start 必须拒绝: %v", err)
	}
	retried := make(chan struct {
		snapshot Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := manager.Retry(context.Background())
		retried <- struct {
			snapshot Snapshot
			err      error
		}{snapshot: snapshot, err: err}
	}()
	request := probe.NextRequest(t)
	if request.Phase != ProbePhaseReadiness || request.Attempt != 2 || request.PID != 512 {
		t.Fatalf("不兼容人工恢复必须重新 readiness: %#v", request)
	}
	probe.Respond(readyProbeResult("2.0.1", "agentd-attempt-2"), nil)
	select {
	case value := <-retried:
		if value.err != nil || value.snapshot.State != StateRunning || value.snapshot.Incompatible {
			t.Fatalf("不兼容人工恢复异常: snapshot=%#v err=%v", value.snapshot, value.err)
		}
	case <-time.After(time.Second):
		t.Fatal("不兼容人工恢复未返回")
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerDegradesRecoversAndRestartsAfterHeartbeatThreshold(t *testing.T) {
	clock := newProcessTestClock(false)
	first := newScriptedProcess(521)
	second := newScriptedProcess(522)
	probe := newControlledProbe()
	observer := &processRecordingObserver{}
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}, clock)
	spec := supervisedSpec(observer, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: time.Second, MaxBackoff: time.Second,
		Multiplier: 1, Window: time.Minute, MaxRestarts: 2, StableAfter: time.Minute,
	})
	spec.Probe = testProbeConfig(probe)
	spec.Probe.FailureThreshold = 2

	started := make(chan error, 1)
	go func() {
		_, err := manager.Start(context.Background(), spec)
		started <- err
	}()
	probe.NextRequest(t)
	probe.Respond(readyProbeResult("1.4.0", "agentd-attempt-1"), nil)
	if err := <-started; err != nil {
		t.Fatal(err)
	}

	clock.WaitForTicker(t)
	clock.Advance(time.Second)
	heartbeat := probe.NextRequest(t)
	if heartbeat.Phase != ProbePhaseHeartbeat || heartbeat.Attempt != 1 {
		t.Fatalf("首次心跳请求异常: %#v", heartbeat)
	}
	probe.Respond(ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.heartbeat_unavailable"}, errors.New("temporary unavailable"))
	waitForProcessState(t, manager, StateDegraded, 521)
	if manager.Status().ConsecutiveProbeFailures != 1 {
		t.Fatalf("首次心跳失败计数异常: %#v", manager.Status())
	}

	clock.Advance(time.Second)
	probe.NextRequest(t)
	probe.Respond(readyProbeResult("1.4.0", "agentd-attempt-1"), nil)
	waitForProcessState(t, manager, StateRunning, 521)
	if manager.Status().ConsecutiveProbeFailures != 0 {
		t.Fatalf("恢复后心跳失败计数未清零: %#v", manager.Status())
	}

	for failure := 1; failure <= 2; failure++ {
		clock.Advance(time.Second)
		probe.NextRequest(t)
		probe.Respond(ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.heartbeat_unavailable"}, errors.New("heartbeat failed"))
		if failure == 1 {
			waitForProcessState(t, manager, StateDegraded, 521)
		}
	}

	clock.WaitForTimer(t)
	clock.Advance(time.Second)
	restartedReadiness := probe.NextRequest(t)
	if restartedReadiness.Phase != ProbePhaseReadiness || restartedReadiness.Attempt != 2 || restartedReadiness.PID != 522 {
		t.Fatalf("重启后必须重新 readiness: %#v", restartedReadiness)
	}
	probe.Respond(readyProbeResult("1.4.1", "agentd-attempt-2"), nil)
	waitForProcessState(t, manager, StateRunning, 522)
	if !first.Interrupted() || manager.Status().RestartCount != 1 {
		t.Fatalf("心跳阈值未触发受控重启: firstInterrupted=%v snapshot=%#v", first.Interrupted(), manager.Status())
	}
	assertProbeMetadata(t, manager.Status(), "1.4.1", "agentd-attempt-2")
	observer.AssertContainsReason(t, ReasonHeartbeatFailed)
	observer.AssertContainsReason(t, ReasonHeartbeatRecovered)
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerValidatesProbeConfigurationAndContainsProbePanic(t *testing.T) {
	invalid := supervisedSpec(nil, RestartConfig{})
	invalid.Probe = ProbeConfig{ReadinessTimeout: time.Second}
	if _, err := newTestProcessManager(&scriptedLauncher{}, newProcessTestClock(true)).Start(context.Background(), invalid); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("缺少 Probe 的配置必须拒绝: %v", err)
	}

	child := newScriptedProcess(531)
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: child}}}, newProcessTestClock(true))
	spec := supervisedSpec(nil, RestartConfig{Policy: RestartNever})
	spec.Probe = testProbeConfig(ProbeFunc(func(context.Context, ProbeRequest) (ProbeResult, error) {
		panic("probe panic")
	}))
	spec.Probe.ReadinessTimeout = time.Second
	if _, err := manager.Start(context.Background(), spec); !errors.Is(err, ErrReadinessFailed) {
		t.Fatalf("Probe panic 必须转换为 readiness 失败: %v", err)
	}
}

func TestManagerReadinessTimeoutUsesRestartPolicyAndEntersCrashLoop(t *testing.T) {
	clock := newProcessTestClock(true)
	first := newScriptedProcess(541)
	second := newScriptedProcess(542)
	launcher := &scriptedLauncher{results: []launchResult{{process: first}, {process: second}}}
	manager := newTestProcessManager(launcher, clock)
	spec := supervisedSpec(nil, RestartConfig{
		Policy: RestartOnFailure, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 10 * time.Millisecond,
		Multiplier: 1, Window: time.Second, MaxRestarts: 1, StableAfter: time.Second,
	})
	spec.Probe = ProbeConfig{
		Probe: ProbeFunc(func(context.Context, ProbeRequest) (ProbeResult, error) {
			return ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.endpoint_pending"}, nil
		}),
		ReadinessTimeout: 20 * time.Millisecond, ReadinessInterval: 10 * time.Millisecond,
		ProbeTimeout: time.Millisecond, HeartbeatInterval: 50 * time.Millisecond, FailureThreshold: 2,
	}
	snapshot, err := manager.Start(context.Background(), spec)
	if !errors.Is(err, ErrCrashLoop) || snapshot.State != StateCrashLoop || !snapshot.CrashLoop || launcher.CallCount() != 2 {
		t.Fatalf("readiness 超时未进入 crash-loop: snapshot=%#v calls=%d err=%v", snapshot, launcher.CallCount(), err)
	}
	if !first.Interrupted() || !second.Interrupted() {
		t.Fatalf("readiness 超时必须终止每个失败 attempt: first=%v second=%v", first.Interrupted(), second.Interrupted())
	}
}

func TestManagerCancelsBlockedReadinessWithStartContext(t *testing.T) {
	child := newScriptedProcess(551)
	probe := newControlledProbe()
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: child}}}, newProcessTestClock(false))
	spec := supervisedSpec(nil, RestartConfig{Policy: RestartNever})
	spec.Probe = testProbeConfig(probe)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Start(ctx, spec)
		result <- err
	}()
	probe.NextRequest(t)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start 取消错误异常: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start 取消未终止阻塞 readiness")
	}
	if !child.Interrupted() || manager.Status().State != StateStopped {
		t.Fatalf("取消后进程未收敛: interrupted=%v snapshot=%#v", child.Interrupted(), manager.Status())
	}
}

func TestManagerAcceptsProtocolReadyButDegradedService(t *testing.T) {
	child := newScriptedProcess(561)
	probe := newControlledProbe()
	manager := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: child}}}, newProcessTestClock(false))
	spec := supervisedSpec(nil, RestartConfig{Policy: RestartNever})
	spec.Probe = testProbeConfig(probe)
	result := make(chan struct {
		snapshot Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := manager.Start(context.Background(), spec)
		result <- struct {
			snapshot Snapshot
			err      error
		}{snapshot: snapshot, err: err}
	}()
	probe.NextRequest(t)
	degraded := readyProbeResult("1.4.0", "agentd-degraded")
	degraded.Status = ProbeStatusDegraded
	degraded.ReasonCode = "process.dependencies_degraded"
	probe.Respond(degraded, nil)
	value := <-result
	if value.err != nil || value.snapshot.State != StateDegraded || !value.snapshot.Ready || !value.snapshot.Running || !value.snapshot.Degraded {
		t.Fatalf("协议可用但服务降级状态异常: snapshot=%#v err=%v", value.snapshot, value.err)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func testProbeConfig(probe Probe) ProbeConfig {
	return ProbeConfig{
		Probe: probe, ReadinessTimeout: 5 * time.Second, ReadinessInterval: time.Second,
		ProbeTimeout: time.Second, HeartbeatInterval: time.Second, FailureThreshold: 3,
	}
}

func readyProbeResult(version string, instanceID string) ProbeResult {
	return ProbeResult{
		Status: ProbeStatusReady, ReasonCode: ReasonReady, ProcessVersion: version,
		ProtocolMajor: 1, ProtocolMinor: 0, InstanceID: instanceID,
		Capabilities: []string{"system.health", "task.execute"},
	}
}

func assertProbeMetadata(t *testing.T, snapshot Snapshot, version string, instanceID string) {
	t.Helper()
	if snapshot.ProcessVersion != version || snapshot.InstanceID != instanceID || snapshot.ProtocolMajor != 1 ||
		snapshot.ProtocolMinor != 0 || len(snapshot.Capabilities) != 2 || snapshot.LastProbeAt.IsZero() || snapshot.LastHealthyAt.IsZero() {
		t.Fatalf("Probe 元数据异常: %#v", snapshot)
	}
}

type controlledProbe struct {
	requests  chan ProbeRequest
	responses chan controlledProbeResponse
	once      sync.Once
}

type controlledProbeResponse struct {
	result ProbeResult
	err    error
}

func newControlledProbe() *controlledProbe {
	return &controlledProbe{requests: make(chan ProbeRequest, 16), responses: make(chan controlledProbeResponse, 16)}
}

func (probe *controlledProbe) Check(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	select {
	case probe.requests <- request:
	case <-ctx.Done():
		return ProbeResult{}, ctx.Err()
	}
	select {
	case response := <-probe.responses:
		return response.result, response.err
	case <-ctx.Done():
		return ProbeResult{}, ctx.Err()
	}
}

func (probe *controlledProbe) NextRequest(t *testing.T) ProbeRequest {
	t.Helper()
	select {
	case request := <-probe.requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("等待 Probe 请求超时")
		return ProbeRequest{}
	}
}

func (probe *controlledProbe) Respond(result ProbeResult, err error) {
	probe.responses <- controlledProbeResponse{result: result, err: err}
}
