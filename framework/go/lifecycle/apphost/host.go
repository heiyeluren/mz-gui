package apphost

import (
	"context"
	"errors"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type Options struct {
	Sidecars SidecarRuntime
	Windows  *WindowGroup
	Clock    frameworkclock.Clock
	Observer Observer
}

type Host struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	sidecars   SidecarRuntime
	windows    *WindowGroup
	clock      frameworkclock.Clock
	observer   Observer
	state      HostState
	running    bool
	degraded   bool
	suspended  bool
	shutdown   bool
	reason     string
	observedAt time.Time
	sequence   uint64
}

func New(options Options) (*Host, error) {
	if options.Sidecars == nil {
		return nil, ErrInvalidHost
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	windows := options.Windows
	if windows == nil {
		var err error
		windows, err = NewWindowGroup([]string{"main"}, WindowGroupOptions{Clock: clock})
		if err != nil {
			return nil, err
		}
	}
	now := clock.Now().UTC()
	return &Host{
		sidecars: options.Sidecars, windows: windows, clock: clock, observer: options.Observer,
		state: StateCreated, reason: "apphost.created", observedAt: now,
	}, nil
}

func (host *Host) Start(ctx context.Context) error {
	if host == nil || ctx == nil {
		return ErrInvalidHost
	}
	host.opMu.Lock()
	defer host.opMu.Unlock()
	if host.shutdown {
		return nil
	}
	if host.running {
		return nil
	}
	host.transition(ctx, "apphost.starting", StateStarting, "apphost.starting")
	if err := host.sidecars.Start(ctx); err != nil {
		host.mu.Lock()
		host.degraded = true
		host.mu.Unlock()
		host.transition(ctx, "apphost.degraded", StateDegraded, "apphost.sidecar_start_failed")
		return err
	}
	host.mu.Lock()
	host.running = true
	host.suspended = false
	host.degraded = false
	host.mu.Unlock()
	host.transition(ctx, "apphost.running", StateRunning, "apphost.ready")
	return nil
}

func (host *Host) Stop(ctx context.Context) error {
	if host == nil || ctx == nil {
		return ErrInvalidHost
	}
	host.opMu.Lock()
	defer host.opMu.Unlock()
	return host.stopLocked(ctx, StateStopped, "apphost.stopped")
}

// RestartSidecar 恢复一个受管后台，保留窗口及应用生命周期。
func (host *Host) RestartSidecar(ctx context.Context, id string) error {
	if host == nil || ctx == nil {
		return ErrInvalidHost
	}
	host.opMu.Lock()
	defer host.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if host.shutdown || host.suspended {
		return ErrInvalidHost
	}
	runtime, ok := host.sidecars.(interface {
		Restart(context.Context, string) error
	})
	if !ok {
		return ErrInvalidHost
	}
	if err := runtime.Restart(ctx, id); err != nil {
		host.markDegraded(ctx, "apphost.restart_failed", "apphost.sidecar_restart_failed")
		return err
	}
	host.mu.Lock()
	host.running = true
	host.degraded = false
	host.mu.Unlock()
	host.transition(ctx, "apphost.recovered", StateRunning, "apphost.sidecar_restarted")
	return nil
}

func (host *Host) HandleSystemEvent(ctx context.Context, kind SystemEventKind) error {
	if host == nil || ctx == nil || !validSystemEvent(kind) {
		return ErrInvalidHost
	}
	host.opMu.Lock()
	defer host.opMu.Unlock()
	switch kind {
	case SystemForeground:
		host.transition(ctx, string(kind), StateRunning, "apphost.foreground")
	case SystemBackground:
		host.transition(ctx, string(kind), StateBackground, "apphost.background")
	case SystemWillSleep:
		if runtime, ok := host.sidecars.(SuspendableRuntime); ok {
			if err := runtime.Suspend(ctx); err != nil {
				host.markDegraded(ctx, string(kind), "apphost.suspend_failed")
				return err
			}
		}
		host.mu.Lock()
		host.suspended = true
		host.mu.Unlock()
		host.transition(ctx, string(kind), StateSuspended, "apphost.suspended")
	case SystemDidWake:
		if runtime, ok := host.sidecars.(SuspendableRuntime); ok {
			if err := runtime.Resume(ctx); err != nil {
				host.markDegraded(ctx, string(kind), "apphost.resume_failed")
				return err
			}
		}
		host.mu.Lock()
		host.suspended = false
		host.mu.Unlock()
		host.transition(ctx, string(kind), StateRunning, "apphost.resumed")
	case SystemShutdown:
		return host.stopLocked(ctx, StateShuttingDown, "apphost.shutdown")
	}
	return ctx.Err()
}

func (host *Host) ApplyWindowEvent(ctx context.Context, event WindowEvent) (WindowSnapshot, error) {
	if host == nil || ctx == nil {
		return WindowSnapshot{}, ErrInvalidHost
	}
	snapshot, err := host.windows.Apply(event)
	if err != nil {
		return WindowSnapshot{}, err
	}
	host.emit(ctx, "apphost.window", host.stateValue(), host.stateValue(), string(event.Kind))
	return snapshot, ctx.Err()
}

func (host *Host) Snapshot() HostSnapshot {
	if host == nil {
		return HostSnapshot{}
	}
	host.mu.RLock()
	state := host.state
	running := host.running
	degraded := host.degraded
	suspended := host.suspended
	shutdown := host.shutdown
	reason := host.reason
	observedAt := host.observedAt
	host.mu.RUnlock()
	sidecars := host.sidecars.Snapshot()
	ready := running && sidecars.Ready && !degraded && !suspended && !shutdown
	return HostSnapshot{
		State: state, Running: running, Ready: ready, Degraded: degraded || sidecars.Degraded,
		Suspended: suspended, ShuttingDown: shutdown, LastReason: reason, ObservedAt: observedAt,
		Sidecars: sidecars, Windows: host.windows.Snapshot(),
	}
}

func (host *Host) stopLocked(ctx context.Context, finalState HostState, reason string) error {
	if host.shutdown && host.stateValue() == StateShuttingDown {
		return nil
	}
	host.mu.Lock()
	host.shutdown = true
	host.running = false
	host.suspended = false
	host.mu.Unlock()
	host.transition(ctx, "apphost.stopping", StateStopping, "apphost.stopping")
	err := host.sidecars.Stop(context.WithoutCancel(ctx))
	if err != nil {
		host.markDegraded(ctx, "apphost.stop_failed", "apphost.sidecar_stop_failed")
	}
	host.transition(ctx, string(finalState), finalState, reason)
	return errors.Join(err, ctx.Err())
}

func (host *Host) markDegraded(ctx context.Context, kind, reason string) {
	host.mu.Lock()
	host.degraded = true
	host.mu.Unlock()
	host.transition(ctx, kind, StateDegraded, reason)
}

func (host *Host) transition(ctx context.Context, kind string, next HostState, reason string) {
	previous := host.stateValue()
	host.mu.Lock()
	host.state = next
	host.reason = reason
	now := host.clock.Now().UTC()
	if !now.After(host.observedAt) {
		now = host.observedAt.Add(time.Nanosecond)
	}
	host.observedAt = now
	host.mu.Unlock()
	host.emit(ctx, kind, previous, next, reason)
}

func (host *Host) emit(ctx context.Context, kind string, from, to HostState, reason string) {
	host.mu.Lock()
	host.sequence++
	event := Event{
		Sequence: host.sequence, Kind: kind, From: from, To: to, ReasonCode: reason, ObservedAt: host.observedAt,
	}
	observer := host.observer
	host.mu.Unlock()
	if observer != nil {
		notifyObserver(context.WithoutCancel(ctx), observer, event)
	}
}

func (host *Host) stateValue() HostState {
	host.mu.RLock()
	defer host.mu.RUnlock()
	return host.state
}

func validSystemEvent(kind SystemEventKind) bool {
	switch kind {
	case SystemForeground, SystemBackground, SystemWillSleep, SystemDidWake, SystemShutdown:
		return true
	default:
		return false
	}
}

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	defer func() { _ = recover() }()
	observer.OnHostEvent(ctx, event)
}
