package managedprocess

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHeartbeatRequiresContinuousFailureWindowAndSuccessResetsIt(t *testing.T) {
	clock := newProcessTestClock(false)
	child := newScriptedProcess(701)
	probe := newControlledProbe()
	m := newTestProcessManager(&scriptedLauncher{results: []launchResult{{process: child}}}, clock)
	spec := supervisedSpec(nil, RestartConfig{Policy: RestartNever})
	spec.Probe = testProbeConfig(probe)
	spec.Probe.FailureThreshold = 2
	spec.Probe.MinFailureDuration = 10 * time.Second
	started := make(chan error, 1)
	go func() { _, err := m.Start(context.Background(), spec); started <- err }()
	probe.NextRequest(t)
	probe.Respond(readyProbeResult("1", "instance-1"), nil)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	clock.WaitForTicker(t)
	fail := func(step time.Duration, count int) {
		clock.Advance(step)
		probe.NextRequest(t)
		probe.Respond(ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.timeout"}, errors.New("timeout"))
		deadline := time.Now().Add(time.Second)
		for m.Status().ConsecutiveProbeFailures < count && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if child.Interrupted() {
			t.Fatal("短时心跳超时不应杀死业务后台")
		}
	}
	fail(time.Second, 1)
	fail(time.Second, 2)
	clock.Advance(7 * time.Second)
	probe.NextRequest(t)
	probe.Respond(readyProbeResult("1", "instance-1"), nil)
	waitForProcessState(t, m, StateRunning, 701)
	fail(time.Second, 1)
	fail(9*time.Second, 2)
	clock.Advance(time.Second)
	probe.NextRequest(t)
	probe.Respond(ProbeResult{Status: ProbeStatusNotReady, ReasonCode: "process.timeout"}, errors.New("timeout"))
	waitForProcessState(t, m, StateStopped, 0)
	if !child.Interrupted() {
		t.Fatal("持续失联达到窗口后必须执行受控停止")
	}
}
