package resilience

import (
	"context"
	"sync"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

const (
	maximumLimiterCapacity = 100_000_000
	maximumRefillInterval  = 24 * time.Hour
)

type Limiter struct {
	mu sync.Mutex

	config     LimiterConfig
	available  int
	lastRefill time.Time
}

func NewLimiter(config LimiterConfig) (*Limiter, error) {
	if !resilienceNamePattern.MatchString(config.Name) || config.Capacity < 1 || config.Capacity > maximumLimiterCapacity ||
		config.RefillTokens < 1 || config.RefillTokens > config.Capacity ||
		config.RefillInterval < time.Millisecond || config.RefillInterval > maximumRefillInterval {
		return nil, ErrInvalidConfig
	}
	intervalsToFull := int64((config.Capacity + config.RefillTokens - 1) / config.RefillTokens)
	if intervalsToFull > int64(time.Duration(1<<63-1)/config.RefillInterval) {
		return nil, ErrInvalidConfig
	}
	if config.Clock == nil {
		config.Clock = frameworkclock.System{}
	}
	now := config.Clock.Now().UTC()
	return &Limiter{config: config, available: config.Capacity, lastRefill: now}, nil
}

func (limiter *Limiter) Acquire(tokens int) error {
	if limiter == nil {
		return ErrInvalidConfig
	}
	if err := limiter.validateTokens(tokens); err != nil {
		return err
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.refillLocked(limiter.config.Clock.Now().UTC())
	if limiter.available < tokens {
		return ErrRateLimited
	}
	limiter.available -= tokens
	return nil
}

func (limiter *Limiter) Wait(ctx context.Context, tokens int) error {
	if limiter == nil || ctx == nil {
		return ErrInvalidRequest
	}
	if err := limiter.validateTokens(tokens); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := limiter.Acquire(tokens); err == nil {
			return nil
		} else if err != ErrRateLimited {
			return err
		}
		delay, err := limiter.RetryAfter(tokens)
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-limiter.config.Clock.After(delay):
		}
	}
}

func (limiter *Limiter) RetryAfter(tokens int) (time.Duration, error) {
	if limiter == nil {
		return 0, ErrInvalidConfig
	}
	if err := limiter.validateTokens(tokens); err != nil {
		return 0, err
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.config.Clock.Now().UTC()
	limiter.refillLocked(now)
	if limiter.available >= tokens {
		return 0, nil
	}
	missing := tokens - limiter.available
	intervals := (missing + limiter.config.RefillTokens - 1) / limiter.config.RefillTokens
	readyAt := limiter.lastRefill.Add(time.Duration(intervals) * limiter.config.RefillInterval)
	if !readyAt.After(now) {
		return 0, nil
	}
	return readyAt.Sub(now), nil
}

func (limiter *Limiter) Snapshot() LimiterSnapshot {
	if limiter == nil {
		return LimiterSnapshot{}
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.refillLocked(limiter.config.Clock.Now().UTC())
	return LimiterSnapshot{
		Name: limiter.config.Name, Capacity: limiter.config.Capacity, Available: limiter.available,
		RefillTokens: limiter.config.RefillTokens, RefillInterval: limiter.config.RefillInterval,
		LastRefillAt: limiter.lastRefill,
	}
}

func (limiter *Limiter) validateTokens(tokens int) error {
	if tokens < 1 {
		return ErrInvalidRequest
	}
	if tokens > limiter.config.Capacity {
		return ErrRequestTooLarge
	}
	return nil
}

func (limiter *Limiter) refillLocked(now time.Time) {
	if !now.After(limiter.lastRefill) {
		return
	}
	intervals := int64(now.Sub(limiter.lastRefill) / limiter.config.RefillInterval)
	if intervals < 1 {
		return
	}
	missing := limiter.config.Capacity - limiter.available
	intervalsToFull := int64((missing + limiter.config.RefillTokens - 1) / limiter.config.RefillTokens)
	if intervals >= intervalsToFull {
		limiter.available = limiter.config.Capacity
	} else {
		limiter.available += int(intervals) * limiter.config.RefillTokens
	}
	limiter.lastRefill = limiter.lastRefill.Add(time.Duration(intervals) * limiter.config.RefillInterval)
}
