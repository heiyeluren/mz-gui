package managedprocess

import (
	"context"
	"errors"
	"time"
)

type startupResult struct {
	snapshot Snapshot
	err      error
}

type processWaitResult struct {
	code int
	err  error
}

type monitorResult struct {
	wait         processWaitResult
	reason       string
	incompatible bool
	exited       bool
}

func (manager *Manager) supervise(ctx context.Context, generation uint64, current process, done chan struct{}, startup chan<- startupResult) {
	defer close(done)
	startupReported := false
	reportStartup := func(err error) {
		if startupReported {
			return
		}
		startupReported = true
		startup <- startupResult{snapshot: manager.Status(), err: err}
	}

	for {
		wait := waitForProcess(current)
		monitored := monitorResult{}
		if _, launchFailed := current.(failedLaunchProcess); launchFailed {
			monitored = monitorResult{wait: <-wait, exited: true}
		} else {
			monitored = manager.monitor(ctx, current, wait, reportStartup)
		}
		monitored.wait = manager.confirmProcessExit(ctx, current, wait, monitored, reportStartup)
		if monitored.incompatible {
			manager.mu.Lock()
			if manager.generation != generation {
				manager.mu.Unlock()
				return
			}
			manager.process = nil
			manager.mu.Unlock()
			exitedAt := manager.now()
			manager.transition(ctx, EventIncompatible, ReasonIncompatible, func(snapshot *Snapshot) {
				snapshot.State = StateIncompatible
				snapshot.Running = false
				snapshot.Ready = false
				snapshot.Degraded = false
				snapshot.Incompatible = true
				snapshot.PID = 0
				snapshot.ExitCode = monitored.wait.code
				snapshot.ExitedAt = exitedAt
			})
			reportStartup(ErrIncompatible)
			manager.clearRun(generation)
			return
		}

		exitedAt := manager.now()
		failure := monitored.wait.code != 0 || monitored.wait.err != nil || monitored.reason != ""
		reason := monitored.reason
		if reason == "" {
			reason = ReasonExitedClean
			if failure {
				reason = ReasonExitedFailure
			}
		}
		if _, launchFailed := current.(failedLaunchProcess); launchFailed {
			reason = ReasonLaunchFailed
		}
		manager.mu.Lock()
		if manager.generation != generation {
			manager.mu.Unlock()
			return
		}
		manager.process = nil
		manager.mu.Unlock()
		manager.transition(ctx, EventExited, reason, func(snapshot *Snapshot) {
			snapshot.State = StateStopped
			snapshot.Running = false
			snapshot.Ready = false
			snapshot.Degraded = false
			snapshot.PID = 0
			snapshot.ExitedAt = exitedAt
			snapshot.ExitCode = monitored.wait.code
			snapshot.NextRestartAt = time.Time{}
		})
		manager.notifyExit()

		manager.mu.RLock()
		stopRequested := manager.stopRequested
		disabled := manager.disabled
		restart := manager.restart
		startedAt := manager.state.StartedAt
		manager.mu.RUnlock()
		if stopRequested || ctx.Err() != nil {
			manager.finishStopped(ctx, disabled)
			reportStartup(ctx.Err())
			manager.clearRun(generation)
			return
		}
		if !shouldRestart(restart.Policy, failure) {
			manager.transition(ctx, EventStopped, ReasonStopped, func(snapshot *Snapshot) {
				snapshot.State = StateStopped
				snapshot.Running = false
			})
			if monitored.reason == ReasonReadinessTimeout || monitored.reason == ReasonReadinessFailed {
				reportStartup(ErrReadinessFailed)
			} else {
				reportStartup(monitored.wait.err)
			}
			manager.clearRun(generation)
			return
		}

		if !startedAt.IsZero() && exitedAt.Sub(startedAt) >= restart.StableAfter {
			manager.mu.Lock()
			manager.restartWindow = nil
			manager.state.ConsecutiveFailures = 0
			manager.mu.Unlock()
		}
		if manager.recordInstabilityAndReachedLimit(exitedAt, restart) {
			manager.transition(ctx, EventCrashLoop, ReasonCrashLoop, func(snapshot *Snapshot) {
				snapshot.State = StateCrashLoop
				snapshot.CrashLoop = true
				snapshot.Running = false
				snapshot.Ready = false
				snapshot.PID = 0
				snapshot.NextRestartAt = time.Time{}
			})
			reportStartup(ErrCrashLoop)
			manager.clearRun(generation)
			return
		}

		delay := manager.nextBackoff(restart)
		nextRestartAt := exitedAt.Add(delay)
		manager.recordRestart(exitedAt, delay, reason)
		manager.transition(ctx, EventDegraded, ReasonRestartScheduled, func(snapshot *Snapshot) {
			snapshot.State = StateBackoff
			snapshot.NextRestartAt = nextRestartAt
		})
		select {
		case <-ctx.Done():
			manager.mu.RLock()
			disabled = manager.disabled
			manager.mu.RUnlock()
			manager.finishStopped(ctx, disabled)
			reportStartup(ctx.Err())
			manager.clearRun(generation)
			return
		case <-manager.clockValue().After(delay):
		}

		attemptedAt := manager.now()
		manager.transition(ctx, EventStarting, ReasonStarting, func(snapshot *Snapshot) {
			snapshot.State = StateStarting
			snapshot.Attempt++
			snapshot.StartedAt = attemptedAt
			snapshot.NextRestartAt = time.Time{}
			snapshot.Ready = false
			snapshot.Degraded = false
			snapshot.ProbeStatus = ""
			snapshot.ProbeReasonCode = ""
			snapshot.ProcessVersion = ""
			snapshot.ProtocolMajor = 0
			snapshot.ProtocolMinor = 0
			snapshot.InstanceID = ""
			snapshot.Capabilities = nil
			snapshot.LastProbeAt = time.Time{}
			snapshot.LastHealthyAt = time.Time{}
			snapshot.ConsecutiveProbeFailures = 0
		})
		next, launchErr := manager.launch(manager.specValue())
		if launchErr != nil {
			current = failedLaunchProcess{code: -1, err: launchErr}
			continue
		}
		current = next
		manager.mu.Lock()
		manager.process = current
		manager.state.PID = current.PID()
		manager.state.StartedAt = manager.clockValueLocked().Now().UTC()
		manager.mu.Unlock()
	}
}

func (manager *Manager) monitor(ctx context.Context, current process, wait <-chan processWaitResult, reportStartup func(error)) monitorResult {
	spec := manager.specValue()
	if spec.Probe.Probe == nil {
		manager.applyReady(ctx, ProbeResult{Status: ProbeStatusReady, ReasonCode: ReasonReady})
		reportStartup(nil)
		return waitOrCancel(ctx, wait)
	}

	deadline := manager.now().Add(spec.Probe.ReadinessTimeout)
	for {
		if !manager.now().Before(deadline) {
			return monitorResult{reason: ReasonReadinessTimeout}
		}
		request := manager.probeRequest(current, ProbePhaseReadiness)
		result, err := manager.runProbe(ctx, spec.Probe, request)
		if result.Status == ProbeStatusIncompatible {
			manager.recordProbe(result, false)
			return monitorResult{reason: ReasonIncompatible, incompatible: true}
		}
		if err == nil && (result.Status == ProbeStatusReady || result.Status == ProbeStatusDegraded) {
			manager.applyReady(ctx, result)
			reportStartup(nil)
			break
		}
		manager.recordProbe(result, true)
		select {
		case exited := <-wait:
			return monitorResult{wait: exited, reason: ReasonReadinessFailed, exited: true}
		case <-ctx.Done():
			return monitorResult{reason: ReasonStopRequested}
		case <-manager.clockValue().After(spec.Probe.ReadinessInterval):
		}
	}

	ticker, err := manager.clockValue().NewTicker(spec.Probe.HeartbeatInterval)
	if err != nil {
		return monitorResult{reason: ReasonHeartbeatFailed}
	}
	defer ticker.Stop()
	var failureSince time.Time
	for {
		select {
		case exited := <-wait:
			return monitorResult{wait: exited, exited: true}
		case <-ctx.Done():
			return monitorResult{reason: ReasonStopRequested}
		case _, ok := <-ticker.C():
			if !ok {
				return monitorResult{reason: ReasonHeartbeatFailed}
			}
			request := manager.probeRequest(current, ProbePhaseHeartbeat)
			result, probeErr := manager.runProbe(ctx, spec.Probe, request)
			if result.Status == ProbeStatusIncompatible {
				manager.recordProbe(result, false)
				return monitorResult{reason: ReasonIncompatible, incompatible: true}
			}
			if probeErr == nil && result.Status == ProbeStatusReady {
				failureSince = time.Time{}
				manager.recoverHeartbeat(ctx, result)
				continue
			}
			if probeErr == nil && result.Status == ProbeStatusDegraded {
				failureSince = time.Time{}
				manager.degradeHealth(ctx, result)
				continue
			}
			if failureSince.IsZero() {
				failureSince = manager.now()
			}
			if manager.failHeartbeat(ctx, result) >= spec.Probe.FailureThreshold && manager.now().Sub(failureSince) >= spec.Probe.MinFailureDuration {
				return monitorResult{reason: ReasonHeartbeatTerminated}
			}
		}
	}
}

func (manager *Manager) runProbe(ctx context.Context, config ProbeConfig, request ProbeRequest) (ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, config.ProbeTimeout)
	defer cancel()
	result, err := checkProbe(probeCtx, config.Probe, request)
	if err != nil && result.Status != ProbeStatusIncompatible {
		result.Status = ProbeStatusNotReady
		if result.ReasonCode == "" {
			result.ReasonCode = ReasonReadinessFailed
		}
	}
	return result, err
}

func (manager *Manager) probeRequest(current process, phase ProbePhase) ProbeRequest {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return ProbeRequest{ProcessID: manager.state.ProcessID, PID: current.PID(), Attempt: manager.state.Attempt, Phase: phase}
}

func (manager *Manager) applyReady(ctx context.Context, result ProbeResult) {
	kind := EventReady
	reason := ReasonReady
	state := StateRunning
	degraded := false
	if result.Status == ProbeStatusDegraded {
		kind = EventDegraded
		reason = ReasonHealthDegraded
		state = StateDegraded
		degraded = true
	}
	manager.transition(ctx, kind, reason, func(snapshot *Snapshot) {
		manager.applyProbeResult(snapshot, result)
		snapshot.State = state
		snapshot.Running = true
		snapshot.Ready = true
		snapshot.Degraded = degraded
		snapshot.Incompatible = false
		snapshot.ExitedAt = time.Time{}
		snapshot.ExitCode = -1
		snapshot.ConsecutiveProbeFailures = 0
	})
}

func (manager *Manager) recoverHeartbeat(ctx context.Context, result ProbeResult) {
	manager.mu.RLock()
	recovering := manager.state.State == StateDegraded || manager.state.ConsecutiveProbeFailures > 0
	manager.mu.RUnlock()
	if !recovering {
		manager.recordProbe(result, false)
		return
	}
	manager.transition(ctx, EventRecovered, ReasonHeartbeatRecovered, func(snapshot *Snapshot) {
		manager.applyProbeResult(snapshot, result)
		snapshot.State = StateRunning
		snapshot.Running = true
		snapshot.Ready = true
		snapshot.Degraded = false
		snapshot.ConsecutiveProbeFailures = 0
	})
}

func (manager *Manager) degradeHealth(ctx context.Context, result ProbeResult) {
	manager.transition(ctx, EventDegraded, ReasonHealthDegraded, func(snapshot *Snapshot) {
		manager.applyProbeResult(snapshot, result)
		snapshot.State = StateDegraded
		snapshot.Running = true
		snapshot.Ready = true
		snapshot.Degraded = true
		snapshot.ConsecutiveProbeFailures = 0
	})
}

func (manager *Manager) failHeartbeat(ctx context.Context, result ProbeResult) int {
	manager.transition(ctx, EventDegraded, ReasonHeartbeatFailed, func(snapshot *Snapshot) {
		manager.applyProbeResult(snapshot, result)
		snapshot.State = StateDegraded
		snapshot.Running = true
		snapshot.Ready = false
		snapshot.Degraded = true
		snapshot.ConsecutiveProbeFailures++
	})
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.state.ConsecutiveProbeFailures
}

func (manager *Manager) recordProbe(result ProbeResult, failed bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.applyProbeResult(&manager.state, result)
	if failed {
		manager.state.ConsecutiveProbeFailures++
	}
}

func (manager *Manager) applyProbeResult(snapshot *Snapshot, result ProbeResult) {
	now := manager.clockValueLocked().Now().UTC()
	snapshot.ProbeStatus = result.Status
	snapshot.ProbeReasonCode = result.ReasonCode
	snapshot.LastProbeAt = now
	if result.Status == ProbeStatusReady {
		snapshot.LastHealthyAt = now
	}
	if result.ProcessVersion != "" {
		snapshot.ProcessVersion = result.ProcessVersion
		snapshot.ProtocolMajor = result.ProtocolMajor
		snapshot.ProtocolMinor = result.ProtocolMinor
		snapshot.InstanceID = result.InstanceID
		snapshot.Capabilities = append([]string(nil), result.Capabilities...)
	}
}

func waitForProcess(current process) <-chan processWaitResult {
	result := make(chan processWaitResult, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- processWaitResult{code: -1, err: errors.New("受管进程 Wait panic")}
			}
			close(result)
		}()
		code, err := current.Wait()
		result <- processWaitResult{code: code, err: err}
	}()
	return result
}

func waitOrCancel(ctx context.Context, wait <-chan processWaitResult) monitorResult {
	select {
	case result := <-wait:
		return monitorResult{wait: result, exited: true}
	case <-ctx.Done():
		return monitorResult{reason: ReasonStopRequested}
	}
}

// 失败时保留进程及唯一的 Wait 结果，下一次 Stop 重试同一棵树，不能提前发布 stopped。
func (manager *Manager) confirmProcessExit(ctx context.Context, current process, wait <-chan processWaitResult, monitored monitorResult, reportStartup func(error)) processWaitResult {
	result, exited := monitored.wait, monitored.exited
	for {
		var err error
		result, exited, err = manager.terminateAndConfirm(current, wait, result, exited)
		if err == nil {
			return result
		}
		manager.transition(ctx, EventDegraded, ReasonStopRequested, func(snapshot *Snapshot) {
			snapshot.State = StateStopping
			snapshot.Running = true
			snapshot.Ready = false
			manager.stopErr = err
			close(manager.stopFailed)
		})
		manager.mu.RLock()
		retry := manager.stopRetry
		manager.mu.RUnlock()
		reportStartup(err)
		<-retry
	}
}

func (manager *Manager) terminateAndConfirm(current process, wait <-chan processWaitResult, result processWaitResult, exited bool) (processWaitResult, bool, error) {
	var interruptErr error
	if !exited {
		interruptErr = current.Interrupt()
		if interruptErr == nil {
			timer := time.NewTimer(manager.forceKillWaitValue())
			select {
			case result = <-wait:
				exited = true
			case <-timer.C:
			}
			timer.Stop()
		}
	}
	// 入口即使自行退出，也必须清理仍属于该进程组或 Job 的后代。
	if err := current.KillTree(); err != nil {
		return result, exited, errors.Join(interruptErr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), manager.forceKillWaitValue())
	defer cancel()
	if !exited {
		select {
		case result = <-wait:
			exited = true
		case <-ctx.Done():
			return result, false, errors.Join(interruptErr, ErrTerminationTimeout)
		}
	}
	if tree, ok := current.(interface{ WaitTree(context.Context) error }); ok {
		if err := tree.WaitTree(ctx); err != nil {
			return result, exited, errors.Join(interruptErr, err)
		}
	}
	return result, exited, interruptErr
}
