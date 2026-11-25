package taskengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func TestEngineExecuteStartsTaskSpanOnHandlerContext(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	seen := make(chan tracecontext.Context, 1)
	mustRegister(t, registry, "test.echo", Registration{
		Resumable: true,
		Timeout:   time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, _ OutputWriter) ExecutionResult {
			trace, ok := tracecontext.FromContext(ctx)
			if ok {
				seen <- trace
			}
			return ExecutionResult{Summary: json.RawMessage(`{"ok":true}`)}
		}),
	})
	engine := newTestEngine(t, repository, registry, 1)
	task := createTask(t, repository, "engine-trace", 1, time.Now().UTC(), nil)
	startEngine(t, engine)

	var trace tracecontext.Context
	select {
	case trace = <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("handler context 未携带 task span")
	}
	waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	if trace.TraceID == "" || trace.SpanID == "" {
		t.Fatalf("task span 无效: %#v", trace)
	}
}
