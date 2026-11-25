package commandbus

import (
	"context"
	"errors"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
)

var (
	ErrInvalidArgument  = errors.New("命令总线参数无效")
	ErrHandlerExists    = errors.New("命令处理器已存在")
	ErrHandlerNotFound  = errors.New("命令处理器不存在")
	ErrDuplicatePending = errors.New("命令正在执行中")
)

type Command struct {
	Type    string
	Payload any
	Meta    inputmodel.RequestMeta
}

type Result struct {
	CommandType string
	RequestID   string
	Value       any
}

type Handler func(context.Context, Command) (Result, error)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	pending  map[string]struct{}
}

func New() *Bus {
	return &Bus{
		handlers: make(map[string]Handler),
		pending:  make(map[string]struct{}),
	}
}

func (bus *Bus) Register(commandType string, handler Handler) error {
	if commandType == "" || handler == nil {
		return ErrInvalidArgument
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if _, exists := bus.handlers[commandType]; exists {
		return ErrHandlerExists
	}
	bus.handlers[commandType] = handler
	return nil
}

func (bus *Bus) Dispatch(ctx context.Context, command Command) (Result, error) {
	if ctx == nil || command.Type == "" {
		return Result{}, ErrInvalidArgument
	}
	if err := command.Meta.Validate(); err != nil {
		return Result{}, err
	}
	bus.mu.RLock()
	handler, exists := bus.handlers[command.Type]
	bus.mu.RUnlock()
	if !exists {
		return Result{}, ErrHandlerNotFound
	}
	pendingKey := command.Meta.EffectivePendingKey(command.Type)
	if pendingKey != "" {
		if err := bus.acquirePending(pendingKey); err != nil {
			return Result{}, err
		}
		defer bus.releasePending(pendingKey)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := handler(ctx, command)
	if err != nil {
		return Result{}, err
	}
	if result.CommandType == "" {
		result.CommandType = command.Type
	}
	if result.RequestID == "" {
		result.RequestID = command.Meta.RequestID
	}
	return result, nil
}

func (bus *Bus) acquirePending(key string) error {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if _, exists := bus.pending[key]; exists {
		return ErrDuplicatePending
	}
	bus.pending[key] = struct{}{}
	return nil
}

func (bus *Bus) releasePending(key string) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	delete(bus.pending, key)
}
