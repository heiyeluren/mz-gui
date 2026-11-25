package terminal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type observedEvents struct {
	mu     sync.Mutex
	events []ObservationEvent
}

func (o *observedEvents) Observe(e ObservationEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
}
func (o *observedEvents) all() []ObservationEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]ObservationEvent(nil), o.events...)
}

func TestTerminalObserverCapturesBeforeRingEvictionWithoutPage(t *testing.T) {
	root := t.TempDir()
	launcher := &fakeLauncher{}
	observer := &observedEvents{}
	m, err := NewManager(context.Background(), ManagerOptions{Launcher: launcher, AllowedRoots: []string{root}, MaxSessions: 1, BufferBytes: 16, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	info, err := m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "test", Cols: 80, Rows: 24, Observation: map[string]string{"owner": "task-a"}})
	if err != nil {
		t.Fatal(err)
	}
	device := launcher.last().device
	device.emit([]byte(strings.Repeat("0123456789", 100)))
	waitForTerminal(t, func() bool {
		for _, e := range observer.all() {
			if e.Kind == "output" {
				return true
			}
		}
		return false
	})
	if err = m.Write(context.Background(), info.SessionID, []byte("private-password\r")); err != nil {
		t.Fatal(err)
	}
	if err = m.CloseSession(context.Background(), info.SessionID, "user-close"); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, func() bool {
		for _, e := range observer.all() {
			if e.Kind == "exited" {
				return true
			}
		}
		return false
	})
	events := observer.all()
	if events[0].Kind != "launch_requested" || events[1].Kind != "started" {
		t.Fatalf("missing launch: %+v", events)
	}
	output := ""
	for _, e := range events {
		if e.Kind == "output" {
			output += string(e.Data)
		}
		if strings.Contains(string(e.Data), "private-password") {
			t.Fatal("raw keystroke capture")
		}
		if e.Tags["owner"] != "task-a" {
			t.Fatal("lost provenance")
		}
	}
	if len(output) != 1000 {
		t.Fatalf("ring truncates durable observation: %d", len(output))
	}
}

type failingObservedLauncher struct{}

func (failingObservedLauncher) Launch(context.Context, LaunchRequest) (Device, Process, error) {
	return nil, nil, errors.New("launch failed")
}

func TestTerminalObserverRecordsFailedLaunchAndCannotPanicExecution(t *testing.T) {
	root := t.TempDir()
	observer := &observedEvents{}
	m, err := NewManager(context.Background(), ManagerOptions{Launcher: failingObservedLauncher{}, AllowedRoots: []string{root}, MaxSessions: 1, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	_, err = m.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "test", Cols: 80, Rows: 24})
	if err == nil {
		t.Fatal("expected failure")
	}
	events := observer.all()
	if len(events) != 2 || events[0].Kind != "launch_requested" || events[1].Kind != "launch_failed" {
		t.Fatalf("failed launch lost: %+v", events)
	}
	m2, err := NewManager(context.Background(), ManagerOptions{Launcher: &fakeLauncher{}, AllowedRoots: []string{root}, MaxSessions: 1, Observer: ObserverFunc(func(ObservationEvent) { panic("observer failure") })})
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close(context.Background())
	if _, err = m2.Create(context.Background(), SessionSpec{CWD: root, ShellProfile: "system", Scope: "test", Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalObserverRecordsResourceLimitRejection(t *testing.T) {
	root := t.TempDir()
	observer := &observedEvents{}
	m, err := NewManager(context.Background(), ManagerOptions{Launcher: &fakeLauncher{}, AllowedRoots: []string{root}, MaxSessions: 1, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	spec := SessionSpec{CWD: root, ShellProfile: "system", Scope: "test", Cols: 80, Rows: 24}
	if _, err = m.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Create(context.Background(), spec); !errors.Is(err, ErrSessionLimit) {
		t.Fatal("expected resource rejection", err)
	}
	failures := 0
	for _, e := range observer.all() {
		if e.Kind == "launch_failed" {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("failed attempt not recorded: %d", failures)
	}
}
