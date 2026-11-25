package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type EngineOptions struct {
	PollInterval time.Duration
	MisfireGrace time.Duration
	BatchSize    int
	Now          func() time.Time
	OnError      func(error)
}

type Engine struct {
	repository Repository
	options    EngineOptions

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewEngine(repository Repository, options EngineOptions) (*Engine, error) {
	if repository == nil || options.PollInterval < time.Millisecond || options.PollInterval > time.Minute ||
		options.MisfireGrace < 0 || options.MisfireGrace > time.Hour || options.BatchSize < 1 || options.BatchSize > MaxSchedulePage {
		return nil, fmt.Errorf("%w: scheduler engine 选项无效", ErrInvalidArgument)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OnError == nil {
		options.OnError = func(error) {}
	}
	return &Engine{repository: repository, options: options, done: make(chan struct{})}, nil
}

func (engine *Engine) Start(parent context.Context) error {
	if parent == nil {
		return fmt.Errorf("%w: scheduler root context 不能为空", ErrInvalidArgument)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.started {
		return ErrConflict
	}
	ctx, cancel := context.WithCancel(parent)
	engine.started = true
	engine.cancel = cancel
	go engine.loop(ctx)
	return nil
}

func (engine *Engine) Close(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: scheduler close context 不能为空", ErrInvalidArgument)
	}
	engine.mu.Lock()
	if !engine.started {
		engine.mu.Unlock()
		return nil
	}
	engine.cancel()
	done := engine.done
	engine.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (engine *Engine) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: scheduler context 不能为空", ErrInvalidArgument)
	}
	now := engine.options.Now().UTC()
	due, err := engine.repository.Due(ctx, now, engine.options.BatchSize)
	if err != nil {
		return err
	}
	var result error
	for _, schedule := range due {
		plan, planErr := PlanDue(schedule, now, engine.options.MisfireGrace)
		if planErr != nil {
			result = errors.Join(result, planErr)
			continue
		}
		if _, applyErr := engine.repository.ApplyPlan(ctx, schedule, plan, now); applyErr != nil && !errors.Is(applyErr, ErrConflict) {
			result = errors.Join(result, applyErr)
		}
	}
	return result
}

func (engine *Engine) loop(ctx context.Context) {
	defer close(engine.done)
	engine.runAndReport(ctx)
	ticker := time.NewTicker(engine.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			engine.runAndReport(ctx)
		}
	}
}

func (engine *Engine) runAndReport(ctx context.Context) {
	if err := engine.RunOnce(ctx); err != nil && ctx.Err() == nil {
		engine.options.OnError(err)
	}
}
