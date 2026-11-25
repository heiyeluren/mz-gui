package resilience

import (
	"errors"
	"testing"
	"time"
)

func TestRetryBudgetAllowsWithinLimitsAndRejectsOverflow(t *testing.T) {
	budget := RetryBudget{MaxAttempts: 3, MaxElapsed: 2 * time.Second}
	if err := budget.Validate(); err != nil {
		t.Fatal(err)
	}
	if !budget.Allow(1, 0) || !budget.Allow(3, 2*time.Second) {
		t.Fatal("预算内必须允许")
	}
	if budget.Allow(4, time.Second) {
		t.Fatal("超过次数必须拒绝")
	}
	if budget.Allow(2, 3*time.Second) {
		t.Fatal("超过时长必须拒绝")
	}
	if (RetryBudget{}).Allow(1, 0) || (RetryBudget{MaxAttempts: 1, MaxElapsed: -time.Second}).Validate() == nil {
		t.Fatal("非法预算必须失败")
	}
	if !errors.Is((RetryBudget{MaxAttempts: 0}).Validate(), ErrInvalidRetryBudget) {
		t.Fatal("零次数必须无效")
	}
}
