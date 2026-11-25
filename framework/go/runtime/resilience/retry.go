package resilience

import (
	"errors"
	"time"
)

var ErrInvalidRetryBudget = errors.New("重试预算无效")

type RetryBudget struct {
	MaxAttempts int
	MaxElapsed  time.Duration
}

func (budget RetryBudget) Validate() error {
	if budget.MaxAttempts < 1 || budget.MaxElapsed < 0 {
		return ErrInvalidRetryBudget
	}
	return nil
}

func (budget RetryBudget) Allow(attempt int, elapsed time.Duration) bool {
	if budget.Validate() != nil || attempt < 1 || elapsed < 0 {
		return false
	}
	if attempt > budget.MaxAttempts {
		return false
	}
	if budget.MaxElapsed > 0 && elapsed > budget.MaxElapsed {
		return false
	}
	return true
}
