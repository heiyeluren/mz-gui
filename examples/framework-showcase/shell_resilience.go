package main

import (
	"context"
	"fmt"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/resilience"
)

func protectShowcaseShell(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("showcase 韧性上下文无效")
	}
	breaker, err := resilience.NewBreaker(resilience.BreakerConfig{
		Name: "showcase-shell", FailureThreshold: 3, SuccessThreshold: 1,
		OpenTimeout: time.Second, HalfOpenMaxRequests: 1,
	})
	if err != nil {
		return err
	}
	if err := breaker.Execute(ctx, func(context.Context) error { return nil }); err != nil {
		return err
	}
	if !(resilience.RetryBudget{MaxAttempts: 2, MaxElapsed: time.Second}).Allow(1, 0) {
		return fmt.Errorf("showcase 重试预算拒绝首次尝试")
	}
	return nil
}
