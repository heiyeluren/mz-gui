package taskengine

import (
	"errors"
	"testing"
	"time"
)

func TestStateMachineHappyPathIncrementsRevision(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	task := Task{ID: "task-1", State: StatePending, Revision: 1, MaxAttempts: 2}

	task = applyOK(t, task, Command{Kind: CommandLease, Now: now, LeaseOwner: "worker-1", LeaseUntil: now.Add(time.Minute)})
	if task.State != StateLeased || task.LeaseOwner != "worker-1" || task.Revision != 2 {
		t.Fatalf("领取结果错误: %#v", task)
	}
	task = applyOK(t, task, Command{Kind: CommandStart, Now: now.Add(time.Second), LeaseOwner: "worker-1"})
	if task.State != StateRunning || task.AttemptCount != 1 || task.StartedAt == nil || task.Revision != 3 {
		t.Fatalf("启动结果错误: %#v", task)
	}
	task = applyOK(t, task, Command{Kind: CommandSucceed, Now: now.Add(2 * time.Second), LeaseOwner: "worker-1"})
	if task.State != StateSucceeded || task.FinishedAt == nil || task.LeaseOwner != "" || task.Revision != 4 {
		t.Fatalf("完成结果错误: %#v", task)
	}
}

func TestStateMachineRejectsInvalidTransitionWithoutMutation(t *testing.T) {
	original := Task{ID: "task-1", State: StatePending, Revision: 7, MaxAttempts: 1}
	got, err := Apply(original, Command{Kind: CommandSucceed, Now: time.Now().UTC()})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("错误类型 = %v", err)
	}
	if got != original {
		t.Fatalf("非法转换修改了实体: got=%#v want=%#v", got, original)
	}
}

func TestStateMachineFailureSelectsRetryOrTerminalFailure(t *testing.T) {
	now := time.Now().UTC()
	running := Task{ID: "task-1", State: StateRunning, Revision: 3, AttemptCount: 1, MaxAttempts: 2, LeaseOwner: "worker-1"}
	retryAt := now.Add(time.Minute)

	retry := applyOK(t, running, Command{
		Kind: CommandFail, Now: now, LeaseOwner: "worker-1", Retryable: true, RetryAt: retryAt, ErrorCode: "UNAVAILABLE",
	})
	if retry.State != StateRetryWait || retry.NextRetryAt == nil || !retry.NextRetryAt.Equal(retryAt) || retry.ErrorCode != "UNAVAILABLE" {
		t.Fatalf("可重试失败结果错误: %#v", retry)
	}

	exhausted := running
	exhausted.AttemptCount = exhausted.MaxAttempts
	failed := applyOK(t, exhausted, Command{
		Kind: CommandFail, Now: now, LeaseOwner: "worker-1", Retryable: true, RetryAt: retryAt, ErrorCode: "UNAVAILABLE",
	})
	if failed.State != StateFailed || failed.NextRetryAt != nil || failed.FinishedAt == nil {
		t.Fatalf("重试用尽结果错误: %#v", failed)
	}
}

func TestCancellationWinsAgainstLateSuccess(t *testing.T) {
	now := time.Now().UTC()
	running := Task{ID: "task-1", State: StateRunning, Revision: 2, AttemptCount: 1, MaxAttempts: 1, LeaseOwner: "worker-1"}

	cancelling := applyOK(t, running, Command{Kind: CommandRequestCancel, Now: now})
	if cancelling.State != StateRunning || cancelling.CancelRequestedAt == nil {
		t.Fatalf("运行中取消请求状态错误: %#v", cancelling)
	}
	finished := applyOK(t, cancelling, Command{Kind: CommandSucceed, Now: now.Add(time.Second), LeaseOwner: "worker-1"})
	if finished.State != StateCancelled || finished.FinishedAt == nil {
		t.Fatalf("迟到成功覆盖了取消: %#v", finished)
	}
}

func TestCancellationIsImmediateBeforeRunning(t *testing.T) {
	now := time.Now().UTC()
	for _, state := range []State{StatePending, StateLeased, StateRetryWait, StateManualReview} {
		t.Run(string(state), func(t *testing.T) {
			task := Task{ID: "task-1", State: state, Revision: 1, MaxAttempts: 1, LeaseOwner: "worker-1"}
			cancelled := applyOK(t, task, Command{Kind: CommandRequestCancel, Now: now})
			if cancelled.State != StateCancelled || cancelled.FinishedAt == nil || cancelled.LeaseOwner != "" {
				t.Fatalf("取消结果错误: %#v", cancelled)
			}
		})
	}
}

func TestLeaseExpiryAndInterruptedRecovery(t *testing.T) {
	now := time.Now().UTC()
	leased := Task{ID: "task-1", State: StateLeased, Revision: 2, MaxAttempts: 2, LeaseOwner: "worker-1"}
	pending := applyOK(t, leased, Command{Kind: CommandLeaseExpired, Now: now})
	if pending.State != StatePending || pending.LeaseOwner != "" {
		t.Fatalf("过期租约未回到 pending: %#v", pending)
	}

	interrupted := Task{ID: "task-2", State: StateInterrupted, Revision: 4, AttemptCount: 1, MaxAttempts: 2}
	resumed := applyOK(t, interrupted, Command{Kind: CommandRecover, Now: now, Resumable: true})
	if resumed.State != StatePending {
		t.Fatalf("可恢复任务状态错误: %#v", resumed)
	}
	manual := applyOK(t, interrupted, Command{Kind: CommandRecover, Now: now, Resumable: false})
	if manual.State != StateManualReview {
		t.Fatalf("不可安全重放任务状态错误: %#v", manual)
	}
}

func TestRetryWaitOnlyReturnsWhenDue(t *testing.T) {
	now := time.Now().UTC()
	retryAt := now.Add(time.Minute)
	task := Task{ID: "task-1", State: StateRetryWait, Revision: 2, MaxAttempts: 2, NextRetryAt: &retryAt}
	if _, err := Apply(task, Command{Kind: CommandRetryDue, Now: now}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("未到期重试错误 = %v", err)
	}
	pending := applyOK(t, task, Command{Kind: CommandRetryDue, Now: retryAt})
	if pending.State != StatePending || pending.NextRetryAt != nil {
		t.Fatalf("到期重试结果错误: %#v", pending)
	}
}

func TestStateReportsOnlyFinalStatesAsTerminal(t *testing.T) {
	for _, state := range []State{StateSucceeded, StateFailed, StateCancelled} {
		if !state.Terminal() {
			t.Fatalf("%s 应为终态", state)
		}
	}
	for _, state := range []State{StatePending, StateLeased, StateRunning, StateRetryWait, StateInterrupted, StateManualReview} {
		if state.Terminal() {
			t.Fatalf("%s 不应为终态", state)
		}
	}
}

func TestLeaseCommandsEnforceOwnerAndDeadline(t *testing.T) {
	now := time.Now().UTC()
	pending := Task{ID: "task-1", State: StatePending, Revision: 1, MaxAttempts: 1}
	for _, command := range []Command{
		{Kind: CommandLease, Now: now, LeaseUntil: now.Add(time.Minute)},
		{Kind: CommandLease, Now: now, LeaseOwner: "worker-1", LeaseUntil: now},
	} {
		if _, err := Apply(pending, command); !errors.Is(err, ErrPrecondition) {
			t.Fatalf("无效租约错误 = %v", err)
		}
	}

	expires := now.Add(time.Minute)
	leased := Task{ID: "task-1", State: StateLeased, Revision: 2, MaxAttempts: 1, LeaseOwner: "worker-1", LeaseExpiresAt: &expires}
	if _, err := Apply(leased, Command{Kind: CommandStart, Now: now, LeaseOwner: "worker-2"}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("错误 owner 必须拒绝: %v", err)
	}
	if _, err := Apply(leased, Command{Kind: CommandStart, Now: expires, LeaseOwner: "worker-1"}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("过期租约必须拒绝: %v", err)
	}
}

func TestRenewLeaseExtendsOwnedLiveLease(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	for _, state := range []State{StateLeased, StateRunning} {
		task := Task{ID: "task-1", State: state, Revision: 2, MaxAttempts: 2, LeaseOwner: "worker-1", LeaseExpiresAt: &expires}
		renewed := applyOK(t, task, Command{
			Kind: CommandRenewLease, Now: now, LeaseOwner: "worker-1", LeaseUntil: now.Add(2 * time.Minute),
		})
		if renewed.LeaseExpiresAt == nil || !renewed.LeaseExpiresAt.Equal(now.Add(2*time.Minute)) || renewed.Revision != 3 {
			t.Fatalf("续租结果错误: %#v", renewed)
		}
	}
}

func TestRenewLeaseRejectsWrongOwnerAndNonExtension(t *testing.T) {
	now := time.Now().UTC()
	expires := now.Add(time.Minute)
	task := Task{ID: "task-1", State: StateRunning, Revision: 2, MaxAttempts: 2, LeaseOwner: "worker-1", LeaseExpiresAt: &expires}
	for _, command := range []Command{
		{Kind: CommandRenewLease, Now: now, LeaseOwner: "worker-2", LeaseUntil: now.Add(2 * time.Minute)},
		{Kind: CommandRenewLease, Now: now, LeaseOwner: "worker-1", LeaseUntil: expires},
	} {
		if _, err := Apply(task, command); !errors.Is(err, ErrPrecondition) {
			t.Fatalf("无效续租错误 = %v", err)
		}
	}
}

func TestFailureRequiresFutureRetryTime(t *testing.T) {
	now := time.Now().UTC()
	running := Task{ID: "task-1", State: StateRunning, Revision: 2, AttemptCount: 1, MaxAttempts: 2, LeaseOwner: "worker-1"}
	_, err := Apply(running, Command{
		Kind: CommandFail, Now: now, LeaseOwner: "worker-1", Retryable: true, RetryAt: now,
	})
	if !errors.Is(err, ErrPrecondition) {
		t.Fatalf("无效重试时间错误 = %v", err)
	}
}

func TestInterruptionValidatesOwnerAndRecoverySource(t *testing.T) {
	now := time.Now().UTC()
	running := Task{ID: "task-1", State: StateRunning, Revision: 2, AttemptCount: 1, MaxAttempts: 2, LeaseOwner: "worker-1"}
	if _, err := Apply(running, Command{Kind: CommandInterrupt, Now: now, LeaseOwner: "worker-2"}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("错误 owner 必须拒绝 interrupt: %v", err)
	}
	interrupted := applyOK(t, running, Command{Kind: CommandInterrupt, Now: now, LeaseOwner: "worker-1", ErrorCode: "WORKER_STOPPED"})
	if interrupted.State != StateInterrupted || interrupted.ErrorCode != "WORKER_STOPPED" || interrupted.LeaseOwner != "" {
		t.Fatalf("interrupt 结果错误: %#v", interrupted)
	}
	if _, err := Apply(running, Command{Kind: CommandRecover, Now: now, Resumable: true}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("非 interrupted 恢复错误 = %v", err)
	}
}

func TestUnknownZeroTimeAndTerminalCommandsAreRejected(t *testing.T) {
	task := Task{ID: "task-1", State: StateSucceeded, Revision: 4, MaxAttempts: 1}
	if _, err := Apply(task, Command{Kind: CommandRequestCancel}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("零时间错误 = %v", err)
	}
	now := time.Now().UTC()
	if _, err := Apply(task, Command{Kind: "unknown", Now: now}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("未知命令错误 = %v", err)
	}
	if _, err := Apply(task, Command{Kind: CommandRequestCancel, Now: now}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("终态重复取消错误 = %v", err)
	}
	if _, err := Apply(task, Command{Kind: CommandLeaseExpired, Now: now}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("终态租约过期错误 = %v", err)
	}
}

func TestApplyManualRetryResetsFailureAndAddsAttemptSlot(t *testing.T) {
	now := time.Now().UTC()
	finishedAt := now.Add(-time.Minute)
	cancelledAt := now.Add(-2 * time.Minute)
	task := Task{
		ID: "task-1", State: StateFailed, AttemptCount: 2, MaxAttempts: 2,
		FinishedAt: &finishedAt, CancelRequestedAt: &cancelledAt,
		ResultSummaryJSON: `{"partial":true}`, ErrorCode: "FAILED", Revision: 7,
	}
	next, err := Apply(task, Command{Kind: CommandManualRetry, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if next.State != StatePending || next.FinishedAt != nil || next.CancelRequestedAt != nil ||
		next.ResultSummaryJSON != "" || next.ErrorCode != "" || next.MaxAttempts != 3 || next.Revision != 8 {
		t.Fatalf("人工重试转换错误: %#v", next)
	}
	if _, err := Apply(Task{State: StateSucceeded}, Command{Kind: CommandManualRetry, Now: now}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("成功任务人工重试错误 = %v", err)
	}
}

func applyOK(t *testing.T, task Task, command Command) Task {
	t.Helper()
	got, err := Apply(task, command)
	if err != nil {
		t.Fatalf("应用命令 %s: %v", command.Kind, err)
	}
	return got
}
