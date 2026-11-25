// Package apphost 提供产品无关的桌面应用生命周期组合根。
package apphost

import (
	"context"
	"errors"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/lifecycle/sidecargroup"
)

var ErrInvalidHost = errors.New("应用组合根参数无效")

type HostState string

const (
	StateCreated      HostState = "created"
	StateStarting     HostState = "starting"
	StateRunning      HostState = "running"
	StateDegraded     HostState = "degraded"
	StateSuspended    HostState = "suspended"
	StateBackground   HostState = "background"
	StateStopping     HostState = "stopping"
	StateStopped      HostState = "stopped"
	StateShuttingDown HostState = "shutting_down"
)

type SystemEventKind string

const (
	SystemForeground SystemEventKind = "system.foreground"
	SystemBackground SystemEventKind = "system.background"
	SystemWillSleep  SystemEventKind = "system.will_sleep"
	SystemDidWake    SystemEventKind = "system.did_wake"
	SystemShutdown   SystemEventKind = "system.shutdown"
)

type WindowEventKind string

const (
	WindowShow      WindowEventKind = "window.show"
	WindowHide      WindowEventKind = "window.hide"
	WindowFocus     WindowEventKind = "window.focus"
	WindowLostFocus WindowEventKind = "window.lost_focus"
	WindowMove      WindowEventKind = "window.move"
	WindowResize    WindowEventKind = "window.resize"
	WindowDPIChange WindowEventKind = "window.dpi_change"
	WindowMinimize  WindowEventKind = "window.minimize"
	WindowRestore   WindowEventKind = "window.restore"
	WindowClose     WindowEventKind = "window.close"
)

type WindowState string

const (
	WindowUnknown   WindowState = "unknown"
	WindowVisible   WindowState = "visible"
	WindowHidden    WindowState = "hidden"
	WindowFocused   WindowState = "focused"
	WindowMinimized WindowState = "minimized"
	WindowClosed    WindowState = "closed"
)

type Bounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type WindowEvent struct {
	WindowID  string          `json:"window_id"`
	Kind      WindowEventKind `json:"kind"`
	Bounds    Bounds          `json:"bounds"`
	DisplayID string          `json:"display_id,omitempty"`
	Scale     float64         `json:"scale,omitempty"`
}

type WindowSnapshot struct {
	WindowID   string      `json:"window_id"`
	State      WindowState `json:"state"`
	Bounds     Bounds      `json:"bounds"`
	DisplayID  string      `json:"display_id,omitempty"`
	Scale      float64     `json:"scale,omitempty"`
	ObservedAt time.Time   `json:"observed_at"`
}

type WindowGroupSnapshot struct {
	Windows []WindowSnapshot `json:"windows"`
}

type SidecarRuntime interface {
	Start(context.Context) error
	Stop(context.Context) error
	Snapshot() sidecargroup.Snapshot
}

type SuspendableRuntime interface {
	Suspend(context.Context) error
	Resume(context.Context) error
}

type Observer interface {
	OnHostEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (observer ObserverFunc) OnHostEvent(ctx context.Context, event Event) {
	observer(ctx, event)
}

type Event struct {
	Sequence   uint64    `json:"sequence"`
	Kind       string    `json:"kind"`
	From       HostState `json:"from"`
	To         HostState `json:"to"`
	ReasonCode string    `json:"reason_code"`
	ObservedAt time.Time `json:"observed_at"`
}

type HostSnapshot struct {
	State        HostState             `json:"state"`
	Running      bool                  `json:"running"`
	Ready        bool                  `json:"ready"`
	Degraded     bool                  `json:"degraded"`
	Suspended    bool                  `json:"suspended"`
	ShuttingDown bool                  `json:"shutting_down"`
	LastReason   string                `json:"last_reason"`
	ObservedAt   time.Time             `json:"observed_at"`
	Sidecars     sidecargroup.Snapshot `json:"sidecars"`
	Windows      WindowGroupSnapshot   `json:"windows"`
}
