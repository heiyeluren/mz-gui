package main

import (
	"context"
	"time"

	taskv1 "github.com/heiyeluren/mz-gui/examples/framework-agentd-showcase/gen/task/v1"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
)

type taskService struct {
	taskv1.UnimplementedTaskServiceServer
	repository taskengine.Repository
	engine     *taskengine.Engine
}

func newTaskService(repository taskengine.Repository, engine *taskengine.Engine) *taskService {
	return &taskService{repository: repository, engine: engine}
}

func (service *taskService) StartTask(ctx context.Context, request *taskv1.StartTaskRequest) (*taskv1.StartTaskResponse, error) {
	task, _, err := service.repository.Create(ctx, taskengine.CreateRequest{
		Type: "example.long_task", Priority: 50, PayloadVersion: 1, Payload: []byte(request.GetPayload()),
		IdempotencyKey: request.GetIdempotencyKey(), WorkspaceID: "showcase", RequiredCapabilities: []string{"task.write"},
		MaxAttempts: 3, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return &taskv1.StartTaskResponse{TaskId: task.ID, State: string(task.State)}, nil
}

func (service *taskService) GetTask(ctx context.Context, request *taskv1.GetTaskRequest) (*taskv1.GetTaskResponse, error) {
	task, err := service.repository.Get(ctx, request.GetTaskId())
	if err != nil {
		return nil, err
	}
	return &taskv1.GetTaskResponse{TaskId: task.ID, State: string(task.State), ResultSummaryJson: task.ResultSummaryJSON}, nil
}

func (service *taskService) CancelTask(ctx context.Context, request *taskv1.CancelTaskRequest) (*taskv1.CancelTaskResponse, error) {
	task, err := service.engine.CancelIdempotent(ctx, request.GetTaskId(), request.GetReason(), request.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	return &taskv1.CancelTaskResponse{TaskId: task.ID, State: string(task.State)}, nil
}
