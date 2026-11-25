// Package resilience 提供产品无关的熔断与限流运行时。
package resilience

import (
	"errors"
	"regexp"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

var (
	ErrInvalidConfig      = errors.New("韧性策略配置无效")
	ErrInvalidRequest     = errors.New("韧性策略请求无效")
	ErrInvalidOperation   = errors.New("韧性策略操作无效")
	ErrCircuitOpen        = errors.New("熔断器已打开")
	ErrHalfOpenSaturated  = errors.New("熔断器半开探测已满")
	ErrRateLimited        = errors.New("请求已被限流")
	ErrRequestTooLarge    = errors.New("请求令牌超过容量")
	ErrOperationPanic     = errors.New("受保护操作发生异常")
	resilienceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half-open"
)

type Transition struct {
	Name string    `json:"name"`
	From State     `json:"from"`
	To   State     `json:"to"`
	At   time.Time `json:"at"`
}

type BreakerConfig struct {
	Name                string
	FailureThreshold    int
	SuccessThreshold    int
	OpenTimeout         time.Duration
	HalfOpenMaxRequests int
	Clock               frameworkclock.Clock
	IsFailure           func(error) bool
	OnTransition        func(Transition)
}

type BreakerSnapshot struct {
	Name                string     `json:"name"`
	State               State      `json:"state"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	HalfOpenSuccesses   int        `json:"half_open_successes"`
	HalfOpenInFlight    int        `json:"half_open_in_flight"`
	OpenedAt            *time.Time `json:"opened_at,omitempty"`
}

type LimiterConfig struct {
	Name           string
	Capacity       int
	RefillTokens   int
	RefillInterval time.Duration
	Clock          frameworkclock.Clock
}

type LimiterSnapshot struct {
	Name           string        `json:"name"`
	Capacity       int           `json:"capacity"`
	Available      int           `json:"available"`
	RefillTokens   int           `json:"refill_tokens"`
	RefillInterval time.Duration `json:"refill_interval"`
	LastRefillAt   time.Time     `json:"last_refill_at"`
}
