package querybus

import (
	"context"
	"errors"
	"sync"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/inputmodel"
)

var (
	ErrInvalidArgument = errors.New("查询总线参数无效")
	ErrHandlerExists   = errors.New("查询处理器已存在")
	ErrHandlerNotFound = errors.New("查询处理器不存在")
)

type Query struct {
	Type    string
	Payload any
	Meta    inputmodel.RequestMeta
}

type Result struct {
	QueryType string
	RequestID string
	Value     any
}

type Handler func(context.Context, Query) (Result, error)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func New() *Bus {
	return &Bus{handlers: make(map[string]Handler)}
}

func (bus *Bus) Register(queryType string, handler Handler) error {
	if queryType == "" || handler == nil {
		return ErrInvalidArgument
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if _, exists := bus.handlers[queryType]; exists {
		return ErrHandlerExists
	}
	bus.handlers[queryType] = handler
	return nil
}

func (bus *Bus) Ask(ctx context.Context, query Query) (Result, error) {
	if ctx == nil || query.Type == "" {
		return Result{}, ErrInvalidArgument
	}
	if err := query.Meta.Validate(); err != nil {
		return Result{}, err
	}
	bus.mu.RLock()
	handler, exists := bus.handlers[query.Type]
	bus.mu.RUnlock()
	if !exists {
		return Result{}, ErrHandlerNotFound
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result, err := handler(ctx, query)
	if err != nil {
		return Result{}, err
	}
	if result.QueryType == "" {
		result.QueryType = query.Type
	}
	if result.RequestID == "" {
		result.RequestID = query.Meta.RequestID
	}
	return result, nil
}
