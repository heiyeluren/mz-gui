package taskengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resourcepool"
)

type OutputWriter interface {
	Write(context.Context, string, []byte) error
}

type CheckpointWriter interface {
	SaveCheckpoint(context.Context, string, json.RawMessage) (Checkpoint, bool, error)
	LatestCheckpoint(context.Context) (Checkpoint, bool, error)
}

type ExecutionResult struct {
	Summary      []byte
	ErrorCode    string
	ErrorSummary string
	Retryable    bool
	RetryAfter   time.Duration
}

type Handler interface {
	Execute(context.Context, Task, OutputWriter) ExecutionResult
}

type HandlerFunc func(context.Context, Task, OutputWriter) ExecutionResult

func (handler HandlerFunc) Execute(ctx context.Context, task Task, output OutputWriter) ExecutionResult {
	return handler(ctx, task, output)
}

type Registration struct {
	Handler       Handler
	Resumable     bool
	Timeout       time.Duration
	ResourceGroup resourcepool.Group
}

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Registration
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Registration)}
}

func (registry *Registry) Register(taskType string, registration Registration) error {
	if !identifierPattern.MatchString(taskType) || len(taskType) > 128 || registration.Handler == nil ||
		registration.Timeout < 100*time.Millisecond || registration.Timeout > 24*time.Hour {
		return fmt.Errorf("%w: handler 注册信息无效", ErrInvalidArgument)
	}
	if registration.ResourceGroup == "" {
		registration.ResourceGroup = resourcepool.GroupDefault
	}
	if !registration.ResourceGroup.Valid() {
		return fmt.Errorf("%w: handler 资源组无效", ErrInvalidArgument)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.handlers[taskType]; exists {
		return fmt.Errorf("%w: handler %s 已注册", ErrConflict, taskType)
	}
	registry.handlers[taskType] = registration
	return nil
}

func (registry *Registry) Lookup(taskType string) (Registration, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	registration, found := registry.handlers[taskType]
	return registration, found
}

func (registry *Registry) Resumable(task Task) bool {
	registration, found := registry.Lookup(task.Type)
	return found && registration.Resumable
}
