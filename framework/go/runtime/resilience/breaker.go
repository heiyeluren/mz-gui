package resilience

import (
	"context"
	"errors"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

const (
	maximumBreakerThreshold = 1_000_000
	maximumOpenTimeout      = 24 * time.Hour
)

type Breaker struct {
	mu sync.Mutex

	config              BreakerConfig
	state               State
	consecutiveFailures int
	halfOpenSuccesses   int
	halfOpenInFlight    int
	openedAt            time.Time
	generation          uint64
}

type Permit struct {
	breaker    *Breaker
	generation uint64
	once       sync.Once
}

func NewBreaker(config BreakerConfig) (*Breaker, error) {
	if !resilienceNamePattern.MatchString(config.Name) ||
		config.FailureThreshold < 1 || config.FailureThreshold > maximumBreakerThreshold ||
		config.SuccessThreshold < 1 || config.SuccessThreshold > maximumBreakerThreshold ||
		config.OpenTimeout < time.Millisecond || config.OpenTimeout > maximumOpenTimeout ||
		config.HalfOpenMaxRequests < 1 || config.HalfOpenMaxRequests > maximumBreakerThreshold {
		return nil, ErrInvalidConfig
	}
	if config.Clock == nil {
		config.Clock = frameworkclock.System{}
	}
	if config.IsFailure == nil {
		config.IsFailure = func(err error) bool {
			return err != nil && !errors.Is(err, context.Canceled)
		}
	}
	return &Breaker{config: config, state: StateClosed, generation: 1}, nil
}

func (breaker *Breaker) Allow() (*Permit, error) {
	if breaker == nil {
		return nil, ErrInvalidConfig
	}
	now := breaker.config.Clock.Now().UTC()
	var transition *Transition
	breaker.mu.Lock()
	if breaker.state == StateOpen && !now.Before(breaker.openedAt.Add(breaker.config.OpenTimeout)) {
		value := breaker.transitionLocked(StateHalfOpen, now)
		transition = &value
	}
	switch breaker.state {
	case StateOpen:
		breaker.mu.Unlock()
		breaker.notify(transition)
		return nil, ErrCircuitOpen
	case StateHalfOpen:
		if breaker.halfOpenInFlight >= breaker.config.HalfOpenMaxRequests {
			breaker.mu.Unlock()
			breaker.notify(transition)
			return nil, ErrHalfOpenSaturated
		}
		breaker.halfOpenInFlight++
	}
	permit := &Permit{breaker: breaker, generation: breaker.generation}
	breaker.mu.Unlock()
	breaker.notify(transition)
	return permit, nil
}

func (breaker *Breaker) Execute(ctx context.Context, operation func(context.Context) error) (err error) {
	if breaker == nil || ctx == nil || operation == nil {
		return ErrInvalidOperation
	}
	permit, err := breaker.Allow()
	if err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			permit.Done(ErrOperationPanic)
			panic(recovered)
		}
	}()
	err = operation(ctx)
	permit.Done(err)
	return err
}

func (permit *Permit) Done(err error) {
	if permit == nil || permit.breaker == nil {
		return
	}
	permit.once.Do(func() { permit.breaker.complete(permit.generation, err) })
}

func (breaker *Breaker) Snapshot() BreakerSnapshot {
	if breaker == nil {
		return BreakerSnapshot{}
	}
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	result := BreakerSnapshot{
		Name: breaker.config.Name, State: breaker.state, ConsecutiveFailures: breaker.consecutiveFailures,
		HalfOpenSuccesses: breaker.halfOpenSuccesses, HalfOpenInFlight: breaker.halfOpenInFlight,
	}
	if breaker.state == StateOpen {
		openedAt := breaker.openedAt
		result.OpenedAt = &openedAt
	}
	return result
}

func (breaker *Breaker) complete(generation uint64, err error) {
	now := breaker.config.Clock.Now().UTC()
	failure := breaker.config.IsFailure(err)
	var transition *Transition
	breaker.mu.Lock()
	if generation != breaker.generation {
		breaker.mu.Unlock()
		return
	}
	switch breaker.state {
	case StateClosed:
		if failure {
			breaker.consecutiveFailures++
			if breaker.consecutiveFailures >= breaker.config.FailureThreshold {
				value := breaker.transitionLocked(StateOpen, now)
				transition = &value
			}
		} else {
			breaker.consecutiveFailures = 0
		}
	case StateHalfOpen:
		if breaker.halfOpenInFlight > 0 {
			breaker.halfOpenInFlight--
		}
		if failure {
			value := breaker.transitionLocked(StateOpen, now)
			transition = &value
		} else {
			breaker.halfOpenSuccesses++
			if breaker.halfOpenSuccesses >= breaker.config.SuccessThreshold {
				value := breaker.transitionLocked(StateClosed, now)
				transition = &value
			}
		}
	}
	breaker.mu.Unlock()
	breaker.notify(transition)
}

func (breaker *Breaker) transitionLocked(to State, now time.Time) Transition {
	value := Transition{Name: breaker.config.Name, From: breaker.state, To: to, At: now}
	from := breaker.state
	breaker.state = to
	breaker.generation++
	switch to {
	case StateOpen:
		if from == StateHalfOpen {
			breaker.consecutiveFailures = 1
		}
		breaker.halfOpenSuccesses = 0
		breaker.halfOpenInFlight = 0
		breaker.openedAt = now
	case StateHalfOpen:
		breaker.consecutiveFailures = 0
		breaker.halfOpenSuccesses = 0
		breaker.halfOpenInFlight = 0
		breaker.openedAt = time.Time{}
	case StateClosed:
		breaker.consecutiveFailures = 0
		breaker.halfOpenSuccesses = 0
		breaker.halfOpenInFlight = 0
		breaker.openedAt = time.Time{}
	}
	return value
}

func (breaker *Breaker) notify(transition *Transition) {
	if transition == nil || breaker.config.OnTransition == nil {
		return
	}
	defer func() { _ = recover() }()
	breaker.config.OnTransition(*transition)
}
