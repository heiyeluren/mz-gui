package taskengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time { return clock.now }
func (clock fixedClock) After(time.Duration) <-chan time.Time {
	return frameworkclock.System{}.After(time.Millisecond)
}
func (clock fixedClock) NewTicker(time.Duration) (frameworkclock.Ticker, error) {
	return frameworkclock.System{}.NewTicker(time.Millisecond)
}

func TestEngineUsesInjectedClockForStartTime(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true, Timeout: time.Second,
		Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
			return ExecutionResult{Summary: json.RawMessage(`{"ok":true}`)}
		}),
	})
	fixed := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	engine, err := NewEngine(repository, registry, EngineOptions{
		Workers: 1, PollInterval: 5 * time.Millisecond, RetryScanInterval: 5 * time.Millisecond,
		LeaseDuration: time.Second, RecoveryBatch: 100, WorkerPrefix: "clock-worker",
		Clock: fixedClock{now: fixed},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeEngine(t, engine) })
	task := createTask(t, repository, "clock-task", 1, fixed.Add(-time.Second), nil)
	startEngine(t, engine)
	finished := waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	if finished.StartedAt == nil || !finished.StartedAt.Equal(fixed) {
		t.Fatalf("任务开始时间未使用注入 Clock: %#v", finished)
	}
}
