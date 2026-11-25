package apphost

import (
	"regexp"
	"sort"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

var windowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type WindowGroupOptions struct {
	Clock frameworkclock.Clock
}

type WindowGroup struct {
	mu      sync.RWMutex
	clock   frameworkclock.Clock
	windows map[string]WindowSnapshot
}

func NewWindowGroup(windowIDs []string, options WindowGroupOptions) (*WindowGroup, error) {
	if len(windowIDs) == 0 {
		return nil, ErrInvalidHost
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	group := &WindowGroup{clock: clock, windows: make(map[string]WindowSnapshot, len(windowIDs))}
	now := clock.Now().UTC()
	for _, id := range windowIDs {
		if !windowIDPattern.MatchString(id) {
			return nil, ErrInvalidHost
		}
		if _, exists := group.windows[id]; exists {
			return nil, ErrInvalidHost
		}
		group.windows[id] = WindowSnapshot{WindowID: id, State: WindowUnknown, ObservedAt: now}
	}
	return group, nil
}

func (group *WindowGroup) Apply(event WindowEvent) (WindowSnapshot, error) {
	if group == nil || !windowIDPattern.MatchString(event.WindowID) || !validWindowEvent(event.Kind) {
		return WindowSnapshot{}, ErrInvalidHost
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	current, exists := group.windows[event.WindowID]
	if !exists {
		return WindowSnapshot{}, ErrInvalidHost
	}
	current.State = nextWindowState(current.State, event.Kind)
	if event.Bounds.Width > 0 && event.Bounds.Height > 0 {
		current.Bounds = event.Bounds
	}
	if event.DisplayID != "" {
		current.DisplayID = event.DisplayID
	}
	if event.Scale > 0 {
		current.Scale = event.Scale
	}
	current.ObservedAt = group.nowAfter(current.ObservedAt)
	group.windows[event.WindowID] = current
	return current, nil
}

func (group *WindowGroup) Snapshot() WindowGroupSnapshot {
	if group == nil {
		return WindowGroupSnapshot{}
	}
	group.mu.RLock()
	defer group.mu.RUnlock()
	windows := make([]WindowSnapshot, 0, len(group.windows))
	for _, window := range group.windows {
		windows = append(windows, window)
	}
	sort.Slice(windows, func(left, right int) bool { return windows[left].WindowID < windows[right].WindowID })
	return WindowGroupSnapshot{Windows: windows}
}

func (group *WindowGroup) nowAfter(previous time.Time) time.Time {
	now := group.clock.Now().UTC()
	if !previous.IsZero() && !now.After(previous) {
		now = previous.Add(time.Nanosecond)
	}
	return now
}

func validWindowEvent(kind WindowEventKind) bool {
	switch kind {
	case WindowShow, WindowHide, WindowFocus, WindowLostFocus, WindowMove, WindowResize, WindowDPIChange, WindowMinimize, WindowRestore, WindowClose:
		return true
	default:
		return false
	}
}

func nextWindowState(current WindowState, kind WindowEventKind) WindowState {
	switch kind {
	case WindowShow, WindowMove, WindowResize, WindowDPIChange, WindowRestore:
		if current == WindowFocused {
			return WindowFocused
		}
		return WindowVisible
	case WindowFocus:
		return WindowFocused
	case WindowLostFocus:
		if current == WindowFocused {
			return WindowVisible
		}
		return current
	case WindowHide:
		return WindowHidden
	case WindowMinimize:
		return WindowMinimized
	case WindowClose:
		return WindowClosed
	default:
		return current
	}
}
