package taskengine

import (
	"fmt"
	"time"
)

// Apply 纯粹计算任务转换；持久化层负责用 revision 原子提交结果。
func Apply(task Task, command Command) (Task, error) {
	if command.Now.IsZero() {
		return task, fmt.Errorf("%w: 命令时间不能为空", ErrPrecondition)
	}
	next := task
	var err error

	switch command.Kind {
	case CommandLease:
		err = applyLease(&next, command)
	case CommandRenewLease:
		err = applyRenewLease(&next, command)
	case CommandStart:
		err = applyStart(&next, command)
	case CommandSucceed:
		err = applySuccess(&next, command)
	case CommandReconcileSuccess:
		err = applyReconciledSuccess(&next, command)
	case CommandFail:
		err = applyFailure(&next, command)
	case CommandRequestCancel:
		err = applyCancellation(&next, command.Now)
	case CommandLeaseExpired:
		err = applyLeaseExpiry(&next)
	case CommandInterrupt:
		err = applyInterruption(&next, command)
	case CommandRecover:
		err = applyRecovery(&next, command.Resumable)
	case CommandRetryDue:
		err = applyRetryDue(&next, command.Now)
	case CommandManualRetry:
		err = applyManualRetry(&next)
	default:
		err = fmt.Errorf("%w: 未知命令 %q", ErrInvalidTransition, command.Kind)
	}
	if err != nil {
		return task, err
	}
	next.Revision++
	return next, nil
}

func applyManualRetry(task *Task) error {
	if task.State != StateFailed && task.State != StateManualReview {
		return invalid(task.State, CommandManualRetry)
	}
	if task.AttemptCount >= MaxAttempts {
		return fmt.Errorf("%w: 人工重试次数达到上限", ErrResourceExhausted)
	}
	task.State = StatePending
	task.FinishedAt = nil
	task.NextRetryAt = nil
	task.CancelRequestedAt = nil
	task.ResultSummaryJSON = ""
	task.ErrorCode = ""
	if task.MaxAttempts <= task.AttemptCount {
		task.MaxAttempts = task.AttemptCount + 1
	}
	return nil
}

func applyRenewLease(task *Task, command Command) error {
	if task.State != StateLeased && task.State != StateRunning {
		return invalid(task.State, command.Kind)
	}
	if err := requireLease(*task, command); err != nil {
		return err
	}
	if !command.LeaseUntil.After(command.Now) ||
		(task.LeaseExpiresAt != nil && !command.LeaseUntil.After(*task.LeaseExpiresAt)) {
		return fmt.Errorf("%w: 新租约必须延长现有租约", ErrPrecondition)
	}
	task.LeaseExpiresAt = timePointer(command.LeaseUntil)
	return nil
}

func applyLease(task *Task, command Command) error {
	if task.State != StatePending {
		return invalid(task.State, command.Kind)
	}
	if command.LeaseOwner == "" || !command.LeaseUntil.After(command.Now) {
		return fmt.Errorf("%w: 租约 owner 和到期时间无效", ErrPrecondition)
	}
	task.State = StateLeased
	task.LeaseOwner = command.LeaseOwner
	task.LeaseExpiresAt = timePointer(command.LeaseUntil)
	return nil
}

func applyStart(task *Task, command Command) error {
	if task.State != StateLeased {
		return invalid(task.State, command.Kind)
	}
	if err := requireLease(*task, command); err != nil {
		return err
	}
	task.State = StateRunning
	task.AttemptCount++
	if task.StartedAt == nil {
		task.StartedAt = timePointer(command.Now)
	}
	return nil
}

func applySuccess(task *Task, command Command) error {
	if task.State != StateRunning {
		return invalid(task.State, command.Kind)
	}
	if err := requireLease(*task, command); err != nil {
		return err
	}
	if task.CancelRequestedAt != nil {
		finish(task, StateCancelled, command.Now)
		return nil
	}
	task.ResultSummaryJSON = command.ResultSummary
	finish(task, StateSucceeded, command.Now)
	return nil
}

// 外部执行器的完成回执只能收口已失去执行权的任务，不能抢占有效 worker。
func applyReconciledSuccess(task *Task, command Command) error {
	if command.ExpectedRevision < 1 || command.ExpectedRevision != task.Revision || command.ResultSummary == "" || task.CancelRequestedAt != nil {
		return fmt.Errorf("%w: 回执版本、结果或取消状态不匹配", ErrPrecondition)
	}
	if task.State != StateInterrupted && task.State != StateManualReview &&
		!(task.State == StateRunning && task.LeaseExpiresAt != nil && !task.LeaseExpiresAt.After(command.Now)) {
		return invalid(task.State, command.Kind)
	}
	task.ResultSummaryJSON = command.ResultSummary
	task.ErrorCode = ""
	finish(task, StateSucceeded, command.Now)
	return nil
}

func applyFailure(task *Task, command Command) error {
	if task.State != StateRunning {
		return invalid(task.State, command.Kind)
	}
	if err := requireLease(*task, command); err != nil {
		return err
	}
	if task.CancelRequestedAt != nil {
		finish(task, StateCancelled, command.Now)
		return nil
	}
	task.ErrorCode = command.ErrorCode
	if command.Retryable && task.AttemptCount < task.MaxAttempts {
		if !command.RetryAt.After(command.Now) {
			return fmt.Errorf("%w: 重试时间必须晚于当前时间", ErrPrecondition)
		}
		task.State = StateRetryWait
		task.NextRetryAt = timePointer(command.RetryAt)
		clearLease(task)
		return nil
	}
	finish(task, StateFailed, command.Now)
	return nil
}

func applyCancellation(task *Task, now time.Time) error {
	switch task.State {
	case StateRunning:
		if task.CancelRequestedAt == nil {
			task.CancelRequestedAt = timePointer(now)
		}
		return nil
	case StatePending, StateLeased, StateRetryWait, StateManualReview:
		task.CancelRequestedAt = timePointer(now)
		finish(task, StateCancelled, now)
		return nil
	default:
		return invalid(task.State, CommandRequestCancel)
	}
}

func applyLeaseExpiry(task *Task) error {
	if task.State != StateLeased {
		return invalid(task.State, CommandLeaseExpired)
	}
	task.State = StatePending
	clearLease(task)
	return nil
}

func applyInterruption(task *Task, command Command) error {
	if task.State != StateRunning {
		return invalid(task.State, command.Kind)
	}
	if command.LeaseOwner != "" && command.LeaseOwner != task.LeaseOwner {
		return fmt.Errorf("%w: lease owner 不匹配", ErrPrecondition)
	}
	if task.CancelRequestedAt != nil {
		finish(task, StateCancelled, command.Now)
		return nil
	}
	task.State = StateInterrupted
	task.ErrorCode = command.ErrorCode
	clearLease(task)
	return nil
}

func applyRecovery(task *Task, resumable bool) error {
	if task.State != StateInterrupted {
		return invalid(task.State, CommandRecover)
	}
	if resumable {
		task.State = StatePending
	} else {
		task.State = StateManualReview
	}
	return nil
}

func applyRetryDue(task *Task, now time.Time) error {
	if task.State != StateRetryWait {
		return invalid(task.State, CommandRetryDue)
	}
	if task.NextRetryAt == nil || now.Before(*task.NextRetryAt) {
		return fmt.Errorf("%w: 重试尚未到期", ErrPrecondition)
	}
	task.State = StatePending
	task.NextRetryAt = nil
	return nil
}

func requireLease(task Task, command Command) error {
	if command.LeaseOwner == "" || command.LeaseOwner != task.LeaseOwner {
		return fmt.Errorf("%w: lease owner 不匹配", ErrPrecondition)
	}
	if task.LeaseExpiresAt != nil && !task.LeaseExpiresAt.After(command.Now) {
		return fmt.Errorf("%w: lease 已过期", ErrPrecondition)
	}
	return nil
}

func finish(task *Task, state State, now time.Time) {
	task.State = state
	task.FinishedAt = timePointer(now)
	task.NextRetryAt = nil
	clearLease(task)
}

func clearLease(task *Task) {
	task.LeaseOwner = ""
	task.LeaseExpiresAt = nil
}

func invalid(state State, command CommandKind) error {
	return fmt.Errorf("%w: %s 不能执行 %s", ErrInvalidTransition, state, command)
}

func timePointer(value time.Time) *time.Time {
	return &value
}
