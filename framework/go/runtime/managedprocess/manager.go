// Package managedprocess 提供产品无关的受管后台进程生命周期。
package managedprocess

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

var (
	ErrInvalidSpec = errors.New("受管进程参数无效")
	ErrNotRunning  = errors.New("受管进程未运行")
	ErrCrashLoop   = errors.New("受管进程处于崩溃循环")
	ErrDisabled    = errors.New("受管进程已禁用")

	envName   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	processID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

const (
	defaultInitialBackoff = 250 * time.Millisecond
	defaultMaxBackoff     = 30 * time.Second
	defaultRestartWindow  = time.Minute
	defaultStableAfter    = 2 * time.Minute
	defaultMaxRestarts    = 5
	defaultForceKillWait  = 2 * time.Second
)

type process interface {
	PID() int
	Wait() (int, error)
	Interrupt() error
	KillTree() error
}

type launcher interface {
	Launch(Spec) (process, error)
}

type Manager struct {
	opMu    sync.Mutex
	eventMu sync.Mutex
	mu      sync.RWMutex

	launcher      launcher
	clock         frameworkclock.Clock
	forceKillWait time.Duration
	process       process
	runCancel     context.CancelFunc
	loopDone      chan struct{}
	stopFailed    chan struct{}
	stopRetry     chan struct{}
	stopErr       error
	spec          Spec
	restart       RestartConfig
	state         Snapshot
	restartWindow []time.Time
	sequence      uint64
	observedAt    time.Time
	generation    uint64
	stopRequested bool
	disabled      bool
}

func New() *Manager {
	return &Manager{
		launcher: platformLauncher{}, clock: frameworkclock.System{}, forceKillWait: defaultForceKillWait,
		state: Snapshot{State: StateStopped, ExitCode: -1},
	}
}

func (manager *Manager) Start(ctx context.Context, spec Spec) (Snapshot, error) {
	if ctx == nil || manager == nil {
		return Snapshot{}, ErrInvalidSpec
	}
	restart, err := validateSpec(spec)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	manager.opMu.Lock()
	defer manager.opMu.Unlock()
	manager.ensureDefaults()
	current := manager.Status()
	manager.mu.RLock()
	runActive := manager.loopDone != nil
	manager.mu.RUnlock()
	switch current.State {
	case StateStarting, StateRunning, StateDegraded, StateBackoff, StateStopping:
		return current, nil
	case StateCrashLoop:
		return current, ErrCrashLoop
	case StateIncompatible:
		return current, ErrIncompatible
	case StateDisabled:
		return current, ErrDisabled
	}
	if runActive {
		return current, nil
	}
	return manager.startLocked(ctx, spec, restart, false)
}

func (manager *Manager) Retry(ctx context.Context) (Snapshot, error) {
	if ctx == nil || manager == nil {
		return Snapshot{}, ErrInvalidSpec
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	manager.opMu.Lock()
	defer manager.opMu.Unlock()
	manager.ensureDefaults()
	state := manager.Status().State
	if state != StateCrashLoop && state != StateIncompatible {
		return manager.Status(), ErrNotRunning
	}
	manager.mu.RLock()
	spec := manager.spec
	restart := manager.restart
	manager.mu.RUnlock()
	return manager.startLocked(ctx, spec, restart, true)
}

func (manager *Manager) Status() Snapshot {
	if manager == nil {
		return Snapshot{}
	}
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.state.clone()
}

func (manager *Manager) Stop(ctx context.Context) error {
	return manager.stop(ctx, false)
}

func (manager *Manager) Disable(ctx context.Context) error {
	return manager.stop(ctx, true)
}

func (manager *Manager) stop(ctx context.Context, disable bool) error {
	if ctx == nil || manager == nil {
		return ErrInvalidSpec
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	bounded, cancelBounded := context.WithTimeout(ctx, 3*manager.forceKillWaitValue()+100*time.Millisecond)
	defer cancelBounded()
	if err := manager.lockOperation(bounded); err != nil {
		return err
	}
	defer manager.opMu.Unlock()
	manager.ensureDefaults()

	manager.mu.RLock()
	state := manager.state.State
	currentProcess := manager.process
	done := manager.loopDone
	cancel := manager.runCancel
	manager.mu.RUnlock()
	if disable && state == StateDisabled {
		return nil
	}
	if state == StateStopped && currentProcess == nil && done == nil {
		if !disable {
			return ErrNotRunning
		}
		manager.mu.Lock()
		manager.disabled = true
		manager.mu.Unlock()
		manager.transition(ctx, EventDisabled, ReasonDisabled, func(snapshot *Snapshot) {
			snapshot.State = StateDisabled
			snapshot.Disabled = true
			snapshot.Running = false
			snapshot.PID = 0
		})
		return nil
	}
	if state == StateCrashLoop || state == StateIncompatible {
		if !disable {
			return ErrNotRunning
		}
		manager.mu.Lock()
		manager.disabled = true
		manager.mu.Unlock()
		manager.transition(ctx, EventDisabled, ReasonDisabled, func(snapshot *Snapshot) {
			snapshot.State = StateDisabled
			snapshot.Disabled = true
			snapshot.CrashLoop = false
			snapshot.Incompatible = false
			snapshot.Ready = false
			snapshot.Degraded = false
			snapshot.Running = false
		})
		return nil
	}

	manager.mu.Lock()
	manager.stopRequested = true
	manager.disabled = disable
	retry := manager.stopErr != nil
	if manager.stopErr != nil {
		manager.stopErr = nil
		manager.stopFailed = make(chan struct{})
	}
	failed := manager.stopFailed
	retrySignal := manager.stopRetry
	manager.mu.Unlock()
	manager.transition(ctx, EventDegraded, ReasonStopRequested, func(snapshot *Snapshot) {
		if manager.process == nil && (snapshot.State == StateStopped || snapshot.State == StateDisabled) {
			return
		}
		snapshot.State = StateStopping
		snapshot.Running = currentProcess != nil
		snapshot.Disabled = disable
	})
	if cancel != nil {
		cancel()
	}
	if retry {
		retrySignal <- struct{}{}
	}
	if done == nil {
		return ErrTerminationTimeout
	}
	select {
	case <-done:
		return bounded.Err()
	case <-failed:
		return manager.stopError()
	case <-bounded.Done():
		return errors.Join(bounded.Err(), ErrTerminationTimeout, manager.stopError())
	}
}

func (manager *Manager) stopError() error {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.stopErr
}

func (manager *Manager) lockOperation(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if manager.opMu.TryLock() {
			if err := ctx.Err(); err != nil {
				manager.opMu.Unlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (manager *Manager) startLocked(ctx context.Context, spec Spec, restart RestartConfig, recovered bool) (Snapshot, error) {
	spec = cloneSpec(spec)
	probe, err := normalizeProbe(spec.Probe)
	if err != nil {
		return Snapshot{}, err
	}
	spec.Probe = probe
	manager.mu.Lock()
	previous := manager.state.clone()
	manager.spec = spec
	manager.restart = restart
	manager.process = nil
	manager.stopRequested = false
	manager.disabled = false
	manager.restartWindow = nil
	manager.generation++
	generation := manager.generation
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	manager.runCancel = cancel
	manager.loopDone = make(chan struct{})
	manager.stopFailed = make(chan struct{})
	manager.stopRetry = make(chan struct{}, 1)
	manager.stopErr = nil
	done := manager.loopDone
	failed := manager.stopFailed
	manager.state = Snapshot{
		ProcessID: resolvedProcessID(spec), State: StateStopped, ExitCode: -1,
	}
	if recovered {
		manager.state.Attempt = previous.Attempt
		manager.state.RestartCount = previous.RestartCount
		manager.state.RestartHistory = previous.RestartHistory
	}
	manager.mu.Unlock()

	if recovered {
		manager.transition(ctx, EventRecovered, ReasonManualRetry, func(snapshot *Snapshot) {
			snapshot.CrashLoop = false
			snapshot.Incompatible = false
			snapshot.ConsecutiveFailures = 0
		})
	}
	manager.transition(ctx, EventStarting, ReasonStarting, func(snapshot *Snapshot) {
		snapshot.State = StateStarting
		snapshot.Attempt++
		snapshot.StartedAt = manager.clockValueLocked().Now().UTC()
		snapshot.PID = 0
		snapshot.Running = false
		snapshot.Disabled = false
		snapshot.CrashLoop = false
		snapshot.Incompatible = false
		snapshot.Ready = false
		snapshot.Degraded = false
		snapshot.ProbeStatus = ""
		snapshot.ProbeReasonCode = ""
		snapshot.ConsecutiveProbeFailures = 0
		snapshot.NextRestartAt = time.Time{}
	})
	process, err := manager.launch(spec)
	if err != nil {
		failedAt := manager.now()
		manager.transition(ctx, EventExited, ReasonLaunchFailed, func(snapshot *Snapshot) {
			snapshot.State = StateStopped
			snapshot.Running = false
			snapshot.ExitCode = -1
			snapshot.ExitedAt = failedAt
		})
		manager.mu.Lock()
		if manager.generation == generation {
			manager.runCancel = nil
			manager.loopDone = nil
		}
		manager.mu.Unlock()
		close(done)
		return manager.Status(), err
	}
	manager.mu.Lock()
	manager.process = process
	manager.state.PID = process.PID()
	manager.state.StartedAt = manager.clockValueLocked().Now().UTC()
	manager.mu.Unlock()
	startup := make(chan startupResult, 1)
	go manager.supervise(runCtx, generation, process, done, startup)
	select {
	case result := <-startup:
		return result.snapshot, result.err
	case <-ctx.Done():
		cancel()
		select {
		case <-done:
		case <-failed:
		case <-time.After(3*manager.forceKillWaitValue() + 100*time.Millisecond):
			return manager.Status(), errors.Join(ctx.Err(), ErrTerminationTimeout, manager.stopError())
		}
		return manager.Status(), errors.Join(ctx.Err(), manager.stopError())
	}
}

type failedLaunchProcess struct {
	code int
	err  error
}

func (value failedLaunchProcess) PID() int           { return 0 }
func (value failedLaunchProcess) Wait() (int, error) { return value.code, value.err }
func (failedLaunchProcess) Interrupt() error         { return nil }
func (failedLaunchProcess) KillTree() error          { return nil }

func (manager *Manager) finishStopped(ctx context.Context, disabled bool) {
	kind := EventStopped
	reason := ReasonStopped
	state := StateStopped
	if disabled {
		kind = EventDisabled
		reason = ReasonDisabled
		state = StateDisabled
	}
	manager.transition(ctx, kind, reason, func(snapshot *Snapshot) {
		snapshot.State = state
		snapshot.Running = false
		snapshot.Disabled = disabled
		snapshot.CrashLoop = false
		snapshot.Incompatible = false
		snapshot.Ready = false
		snapshot.Degraded = false
		snapshot.PID = 0
		snapshot.NextRestartAt = time.Time{}
	})
}

func (manager *Manager) transition(ctx context.Context, kind EventKind, reason string, update func(*Snapshot)) {
	manager.eventMu.Lock()
	defer manager.eventMu.Unlock()
	manager.mu.Lock()
	update(&manager.state)
	now := manager.clockValueLocked().Now().UTC()
	if !now.After(manager.observedAt) {
		now = manager.observedAt.Add(time.Nanosecond)
	}
	manager.observedAt = now
	manager.sequence++
	event := Event{
		Sequence: manager.sequence, Kind: kind, ProcessID: manager.state.ProcessID, State: manager.state.State,
		PID: manager.state.PID, Attempt: manager.state.Attempt, ExitCode: manager.state.ExitCode,
		ReasonCode: reason, ObservedAt: now, NextRestartAt: manager.state.NextRestartAt,
	}
	manager.state.LastReasonCode = reason
	observer := manager.spec.Observer
	manager.mu.Unlock()
	if observer != nil {
		notifyObserver(context.WithoutCancel(ctx), observer, event)
	}
}

func (manager *Manager) notifyExit() {
	manager.mu.RLock()
	callback := manager.spec.OnExit
	snapshot := manager.state.clone()
	manager.mu.RUnlock()
	if callback == nil {
		return
	}
	defer func() { _ = recover() }()
	callback(snapshot)
}

func (manager *Manager) recordInstabilityAndReachedLimit(now time.Time, restart RestartConfig) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	cutoff := now.Add(-restart.Window)
	kept := manager.restartWindow[:0]
	for _, observedAt := range manager.restartWindow {
		if !observedAt.Before(cutoff) {
			kept = append(kept, observedAt)
		}
	}
	manager.restartWindow = kept
	manager.state.ConsecutiveFailures++
	if len(manager.restartWindow) >= restart.MaxRestarts {
		return true
	}
	manager.restartWindow = append(manager.restartWindow, now)
	return false
}

func (manager *Manager) recordRestart(now time.Time, delay time.Duration, reason string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.state.RestartCount++
	manager.state.RestartHistory = append(manager.state.RestartHistory, RestartRecord{
		Sequence: uint64(manager.state.RestartCount), ScheduledAt: now, Delay: delay, ReasonCode: reason,
	})
	if len(manager.state.RestartHistory) > 64 {
		manager.state.RestartHistory = append([]RestartRecord(nil), manager.state.RestartHistory[len(manager.state.RestartHistory)-64:]...)
	}
}

func (manager *Manager) nextBackoff(restart RestartConfig) time.Duration {
	manager.mu.RLock()
	failures := manager.state.ConsecutiveFailures
	manager.mu.RUnlock()
	exponent := math.Max(0, float64(failures-1))
	value := float64(restart.InitialBackoff) * math.Pow(restart.Multiplier, exponent)
	if value > float64(restart.MaxBackoff) {
		value = float64(restart.MaxBackoff)
	}
	if restart.Jitter > 0 {
		unit := secureRandomUnit()
		value *= 1 - restart.Jitter + 2*restart.Jitter*unit
	}
	if value > float64(restart.MaxBackoff) {
		value = float64(restart.MaxBackoff)
	}
	if value < 1 {
		value = 1
	}
	return time.Duration(value)
}

func (manager *Manager) launch(spec Spec) (process, error) {
	if _, err := validateSpec(spec); err != nil {
		return nil, err
	}
	return manager.launcherValue().Launch(cloneSpec(spec))
}

func (manager *Manager) clearRun(generation uint64) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.generation != generation {
		return
	}
	manager.process = nil
	manager.runCancel = nil
	manager.loopDone = nil
}

func (manager *Manager) ensureDefaults() {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.launcher == nil {
		manager.launcher = platformLauncher{}
	}
	if manager.clock == nil {
		manager.clock = frameworkclock.System{}
	}
	if manager.forceKillWait <= 0 {
		manager.forceKillWait = defaultForceKillWait
	}
	if manager.state.State == "" {
		manager.state = Snapshot{State: StateStopped, ExitCode: -1}
	}
}

func (manager *Manager) clockValue() frameworkclock.Clock {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.clockValueLocked()
}

func (manager *Manager) clockValueLocked() frameworkclock.Clock {
	if manager.clock == nil {
		return frameworkclock.System{}
	}
	return manager.clock
}

func (manager *Manager) launcherValue() launcher {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.launcher == nil {
		return platformLauncher{}
	}
	return manager.launcher
}

func (manager *Manager) forceKillWaitValue() time.Duration {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.forceKillWait <= 0 {
		return defaultForceKillWait
	}
	return manager.forceKillWait
}

func (manager *Manager) specValue() Spec {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneSpec(manager.spec)
}

func (manager *Manager) now() time.Time {
	return manager.clockValue().Now().UTC()
}

func validateSpec(spec Spec) (RestartConfig, error) {
	executable := filepath.Clean(strings.TrimSpace(spec.Executable))
	if !filepath.IsAbs(executable) || len(spec.Arguments) > 64 || len(spec.Environment) > 64 {
		return RestartConfig{}, ErrInvalidSpec
	}
	if !processID.MatchString(resolvedProcessID(spec)) {
		return RestartConfig{}, ErrInvalidSpec
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return RestartConfig{}, ErrInvalidSpec
	}
	if spec.Directory != "" {
		directory := filepath.Clean(spec.Directory)
		info, err := os.Lstat(directory)
		if !filepath.IsAbs(directory) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return RestartConfig{}, ErrInvalidSpec
		}
	}
	for _, argument := range spec.Arguments {
		if len(argument) > 4096 || strings.ContainsRune(argument, 0) {
			return RestartConfig{}, ErrInvalidSpec
		}
	}
	for key, value := range spec.Environment {
		if !envName.MatchString(key) || len(value) > 64<<10 || strings.ContainsRune(value, 0) {
			return RestartConfig{}, ErrInvalidSpec
		}
	}
	restart, err := normalizeRestart(spec.Restart)
	if err != nil {
		return RestartConfig{}, err
	}
	if _, err := normalizeProbe(spec.Probe); err != nil {
		return RestartConfig{}, err
	}
	return restart, nil
}

func normalizeRestart(value RestartConfig) (RestartConfig, error) {
	if value.Policy == "" {
		value.Policy = RestartNever
	}
	if !value.Policy.Valid() || value.InitialBackoff < 0 || value.MaxBackoff < 0 || value.Multiplier < 0 ||
		value.Jitter < 0 || value.Jitter > 1 || value.Window < 0 || value.MaxRestarts < 0 || value.StableAfter < 0 {
		return RestartConfig{}, ErrInvalidSpec
	}
	if value.InitialBackoff == 0 {
		value.InitialBackoff = defaultInitialBackoff
	}
	if value.MaxBackoff == 0 {
		value.MaxBackoff = defaultMaxBackoff
	}
	if value.Multiplier == 0 {
		value.Multiplier = 2
	}
	if value.Window == 0 {
		value.Window = defaultRestartWindow
	}
	if value.MaxRestarts == 0 {
		value.MaxRestarts = defaultMaxRestarts
	}
	if value.StableAfter == 0 {
		value.StableAfter = defaultStableAfter
	}
	if value.InitialBackoff > value.MaxBackoff || value.Multiplier < 1 || value.Window < value.InitialBackoff || value.StableAfter <= 0 {
		return RestartConfig{}, ErrInvalidSpec
	}
	return value, nil
}

func resolvedProcessID(spec Spec) string {
	if spec.ID != "" {
		return spec.ID
	}
	return filepath.Base(spec.Executable)
}

func cloneSpec(spec Spec) Spec {
	spec.Arguments = append([]string(nil), spec.Arguments...)
	if spec.Environment == nil {
		return spec
	}
	environment := make(map[string]string, len(spec.Environment))
	for key, value := range spec.Environment {
		environment[key] = value
	}
	spec.Environment = environment
	return spec
}

func shouldRestart(policy RestartPolicy, failure bool) bool {
	return policy == RestartAlways || policy == RestartOnFailure && failure
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.OnProcessEvent(ctx, event)
}

func secureRandomUnit() float64 {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0.5
	}
	return float64(binary.LittleEndian.Uint64(value[:])>>11) / (1 << 53)
}

func sidecarEnvAllowlist(goos string) []string {
	allowed := []string{"PATH", "LANG", "LC_ALL", "LC_CTYPE"}
	if goos == "linux" {
		allowed = append(allowed, "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY")
	}
	if goos == "windows" {
		return append(allowed, "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "SystemRoot", "WINDIR", "COMSPEC", "TEMP", "TMP", "LOCALAPPDATA", "APPDATA", "USERNAME", "USERDOMAIN", "PATHEXT")
	}
	return append(allowed, "HOME", "USER", "LOGNAME", "TMPDIR", "SSH_AUTH_SOCK")
}

func mergedEnvironment(overrides map[string]string) []string {
	return mergeSidecarEnvironment(runtime.GOOS, os.LookupEnv, overrides)
}

func mergeSidecarEnvironment(goos string, lookup func(string) (string, bool), overrides map[string]string) []string {
	allowed := sidecarEnvAllowlist(goos)
	values := make(map[string]string, len(allowed)+len(overrides))
	for _, key := range allowed {
		if value, exists := lookup(key); exists {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
