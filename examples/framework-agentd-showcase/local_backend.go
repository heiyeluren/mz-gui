package main

import (
	"context"
	"time"

	"github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/internal/taskexample"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
)

type localTaskBackend struct {
	store *taskexample.Store
}

func newLocalTaskBackend(ctx context.Context, runDir string) (*localTaskBackend, error) {
	store, err := taskexample.Open(ctx, runDir)
	if err != nil {
		return nil, err
	}
	return &localTaskBackend{store: store}, nil
}

func (backend *localTaskBackend) StartTask(ctx context.Context, payload, idempotencyKey string) (TaskSnapshot, error) {
	task, _, err := backend.store.Repository.Create(ctx, taskengine.CreateRequest{
		Type: "example.long_task", Priority: 50, PayloadVersion: 1, Payload: []byte(payload),
		IdempotencyKey: idempotencyKey, WorkspaceID: "showcase", RequiredCapabilities: []string{"task.write"},
		MaxAttempts: 3, Now: time.Now().UTC(),
	})
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: task.ID, State: string(task.State)}, nil
}

func (backend *localTaskBackend) GetTask(ctx context.Context, taskID string) (TaskSnapshot, error) {
	task, err := backend.store.Repository.Get(ctx, taskID)
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: task.ID, State: string(task.State), ResultSummaryJSON: task.ResultSummaryJSON}, nil
}

func (backend *localTaskBackend) CancelTask(ctx context.Context, taskID, reason, idempotencyKey string) (TaskSnapshot, error) {
	task, err := backend.store.Engine.CancelIdempotent(ctx, taskID, reason, idempotencyKey)
	if err != nil {
		return TaskSnapshot{}, err
	}
	return TaskSnapshot{ID: task.ID, State: string(task.State)}, nil
}

func (backend *localTaskBackend) Close(ctx context.Context) error {
	return backend.store.Close(ctx)
}
