package lifecycle

import (
	"context"
	"errors"
	"regexp"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

var (
	ErrInvalidArgument       = errors.New("生命周期组件参数无效")
	ErrInvalidGraph          = errors.New("生命周期依赖图无效")
	ErrInvalidContract       = errors.New("生命周期状态契约无效")
	ErrDependencyUnavailable = errors.New("生命周期必需依赖不可用")
	ErrClosed                = errors.New("生命周期监督器已关闭")

	componentNamePattern = regexp.MustCompile("^[a-z0-9][a-z0-9._-]{0,63}$")
)

const defaultPhaseTimeout = 30 * time.Second

const (
	ReasonCreated               = "lifecycle.created"
	ReasonStarting              = "lifecycle.starting"
	ReasonReady                 = "lifecycle.ready"
	ReasonStartFailed           = "lifecycle.start_failed"
	ReasonReadyFailed           = "lifecycle.ready_failed"
	ReasonDependencyUnavailable = "lifecycle.dependency_unavailable"
	ReasonQuiescing             = "lifecycle.quiescing"
	ReasonQuiesceFailed         = "lifecycle.quiesce_failed"
	ReasonStopping              = "lifecycle.stopping"
	ReasonStopped               = "lifecycle.stopped"
	ReasonStopFailed            = "lifecycle.stop_failed"
)

func (state State) Valid() bool {
	switch state {
	case StateCreated, StateStarting, StateReady, StateDegraded, StateFailed, StateCrashLoop, StateQuiescing, StateStopping, StateStopped:
		return true
	default:
		return false
	}
}

type Component interface {
	Name() string
	Start(context.Context) error
	Ready(context.Context) error
	Stop(context.Context) error
}

type Quiescer interface {
	Quiesce(context.Context) error
}

type FailurePolicy string

const (
	FailureRequired FailurePolicy = "required"
	FailureOptional FailurePolicy = "optional"
)

type Timeouts struct {
	Start   time.Duration
	Ready   time.Duration
	Quiesce time.Duration
	Stop    time.Duration
}

type Node struct {
	Component        Component
	Requires         []string
	OptionalRequires []string
	Timeouts         Timeouts
	FailurePolicy    FailurePolicy
}

type Options struct {
	Clock             frameworkclock.Clock
	Observer          Observer
	DefaultTimeouts   Timeouts
	MaxParallelStarts int
}

type State string

const (
	StateCreated   State = "created"
	StateStarting  State = "starting"
	StateReady     State = "ready"
	StateDegraded  State = "degraded"
	StateFailed    State = "failed"
	StateCrashLoop State = "crash_loop"
	StateQuiescing State = "quiescing"
	StateStopping  State = "stopping"
	StateStopped   State = "stopped"
)

type Event struct {
	Sequence    uint64          `json:"sequence"`
	ComponentID model.ID        `json:"component_id"`
	From        State           `json:"from"`
	To          State           `json:"to"`
	ReasonCode  string          `json:"reason_code"`
	ObservedAt  model.Timestamp `json:"observed_at"`
}

type Observer interface {
	OnLifecycleEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (observer ObserverFunc) OnLifecycleEvent(ctx context.Context, event Event) {
	observer(ctx, event)
}

type ComponentSnapshot struct {
	ComponentID model.ID        `json:"component_id"`
	State       State           `json:"state"`
	ReasonCode  string          `json:"reason_code"`
	ObservedAt  model.Timestamp `json:"observed_at"`
}

type Snapshot struct {
	Running    bool                `json:"running"`
	Closed     bool                `json:"closed"`
	Degraded   bool                `json:"degraded"`
	ObservedAt model.Timestamp     `json:"observed_at"`
	Components []ComponentSnapshot `json:"components"`
}
