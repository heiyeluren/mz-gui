package managedprocess

import (
	"context"
	"io"
	"time"
)

type State string

const (
	StateStopped      State = "stopped"
	StateStarting     State = "starting"
	StateRunning      State = "running"
	StateDegraded     State = "degraded"
	StateBackoff      State = "backoff"
	StateCrashLoop    State = "crash_loop"
	StateIncompatible State = "incompatible"
	StateStopping     State = "stopping"
	StateDisabled     State = "disabled"
)

func (state State) Valid() bool {
	switch state {
	case StateStopped, StateStarting, StateRunning, StateDegraded, StateBackoff, StateCrashLoop, StateIncompatible, StateStopping, StateDisabled:
		return true
	default:
		return false
	}
}

type RestartPolicy string

const (
	RestartNever     RestartPolicy = "never"
	RestartOnFailure RestartPolicy = "on-failure"
	RestartAlways    RestartPolicy = "always"
)

func (policy RestartPolicy) Valid() bool {
	return policy == RestartNever || policy == RestartOnFailure || policy == RestartAlways
}

type RestartConfig struct {
	Policy         RestartPolicy `json:"policy"`
	InitialBackoff time.Duration `json:"initial_backoff"`
	MaxBackoff     time.Duration `json:"max_backoff"`
	Multiplier     float64       `json:"multiplier"`
	Jitter         float64       `json:"jitter"`
	Window         time.Duration `json:"window"`
	MaxRestarts    int           `json:"max_restarts"`
	StableAfter    time.Duration `json:"stable_after"`
}

type EventKind string

const (
	EventStarting     EventKind = "process.starting"
	EventReady        EventKind = "process.ready"
	EventExited       EventKind = "process.exited"
	EventDegraded     EventKind = "process.degraded"
	EventCrashLoop    EventKind = "process.crash_loop"
	EventIncompatible EventKind = "process.incompatible"
	EventRecovered    EventKind = "process.recovered"
	EventStopped      EventKind = "process.stopped"
	EventDisabled     EventKind = "process.disabled"
)

func (kind EventKind) Valid() bool {
	switch kind {
	case EventStarting, EventReady, EventExited, EventDegraded, EventCrashLoop, EventIncompatible, EventRecovered, EventStopped, EventDisabled:
		return true
	default:
		return false
	}
}

const (
	ReasonStarting            = "process.starting"
	ReasonReady               = "process.ready"
	ReasonExitedClean         = "process.exited_clean"
	ReasonExitedFailure       = "process.exited_failure"
	ReasonRestartScheduled    = "process.restart_scheduled"
	ReasonLaunchFailed        = "process.launch_failed"
	ReasonCrashLoop           = "process.crash_loop"
	ReasonManualRetry         = "process.manual_retry"
	ReasonStopRequested       = "process.stop_requested"
	ReasonStopped             = "process.stopped"
	ReasonDisabled            = "process.disabled"
	ReasonReadinessTimeout    = "process.readiness_timeout"
	ReasonReadinessFailed     = "process.readiness_failed"
	ReasonProbeInvalid        = "process.probe_invalid"
	ReasonProbePanic          = "process.probe_panic"
	ReasonHealthDegraded      = "process.health_degraded"
	ReasonHeartbeatFailed     = "process.heartbeat_failed"
	ReasonHeartbeatTerminated = "process.heartbeat_terminated"
	ReasonHeartbeatRecovered  = "process.heartbeat_recovered"
	ReasonIncompatible        = "process.incompatible"
)

type RestartRecord struct {
	Sequence    uint64        `json:"sequence"`
	ScheduledAt time.Time     `json:"scheduled_at"`
	Delay       time.Duration `json:"delay"`
	ReasonCode  string        `json:"reason_code"`
}

type Event struct {
	Sequence      uint64    `json:"sequence"`
	Kind          EventKind `json:"kind"`
	ProcessID     string    `json:"process_id"`
	State         State     `json:"state"`
	PID           int       `json:"pid"`
	Attempt       uint64    `json:"attempt"`
	ExitCode      int       `json:"exit_code"`
	ReasonCode    string    `json:"reason_code"`
	ObservedAt    time.Time `json:"observed_at"`
	NextRestartAt time.Time `json:"next_restart_at,omitempty"`
}

type Observer interface {
	OnProcessEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (observer ObserverFunc) OnProcessEvent(ctx context.Context, event Event) {
	observer(ctx, event)
}

type Spec struct {
	ID          string
	Executable  string
	Arguments   []string
	Directory   string
	Environment map[string]string
	Stdout      io.Writer
	Stderr      io.Writer
	Restart     RestartConfig
	Probe       ProbeConfig
	Observer    Observer
	OnExit      func(Snapshot)
}

type Snapshot struct {
	ProcessID                string          `json:"process_id"`
	State                    State           `json:"state"`
	Running                  bool            `json:"running"`
	CrashLoop                bool            `json:"crash_loop"`
	Ready                    bool            `json:"ready"`
	Degraded                 bool            `json:"degraded"`
	Incompatible             bool            `json:"incompatible"`
	Disabled                 bool            `json:"disabled"`
	PID                      int             `json:"pid"`
	Attempt                  uint64          `json:"attempt"`
	StartedAt                time.Time       `json:"started_at"`
	ExitedAt                 time.Time       `json:"exited_at"`
	NextRestartAt            time.Time       `json:"next_restart_at"`
	ExitCode                 int             `json:"exit_code"`
	RestartCount             int             `json:"restart_count"`
	ConsecutiveFailures      int             `json:"consecutive_failures"`
	LastReasonCode           string          `json:"last_reason_code"`
	ProbeStatus              ProbeStatus     `json:"probe_status,omitempty"`
	ProbeReasonCode          string          `json:"probe_reason_code,omitempty"`
	ProcessVersion           string          `json:"process_version,omitempty"`
	ProtocolMajor            uint32          `json:"protocol_major,omitempty"`
	ProtocolMinor            uint32          `json:"protocol_minor,omitempty"`
	InstanceID               string          `json:"instance_id,omitempty"`
	Capabilities             []string        `json:"capabilities,omitempty"`
	LastProbeAt              time.Time       `json:"last_probe_at,omitempty"`
	LastHealthyAt            time.Time       `json:"last_healthy_at,omitempty"`
	ConsecutiveProbeFailures int             `json:"consecutive_probe_failures"`
	RestartHistory           []RestartRecord `json:"restart_history"`
}

func (snapshot Snapshot) clone() Snapshot {
	snapshot.RestartHistory = append([]RestartRecord(nil), snapshot.RestartHistory...)
	snapshot.Capabilities = append([]string(nil), snapshot.Capabilities...)
	return snapshot
}
