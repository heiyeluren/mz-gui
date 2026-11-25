package taskengine

import (
	"context"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func startTaskSpan(ctx context.Context, task Task) (context.Context, tracecontext.Span, error) {
	attributes := map[string]string{}
	if task.Type != "" {
		attributes["task_type"] = task.Type
	}
	return tracecontext.StartSpan(ctx, tracecontext.SpanRequest{
		Kind:           tracecontext.OperationTask,
		Component:      "taskengine",
		Operation:      "task.execute",
		Sampled:        true,
		SafeAttributes: attributes,
	})
}
