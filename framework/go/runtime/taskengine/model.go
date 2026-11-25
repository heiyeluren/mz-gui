package taskengine

import (
	"errors"
	"time"
)

var (
	ErrInvalidTransition = errors.New("任务状态转换无效")
	ErrPrecondition      = errors.New("任务前置条件不满足")
	ErrInvalidArgument   = errors.New("任务参数无效")
	ErrNotFound          = errors.New("任务不存在")
	ErrConflict          = errors.New("任务发生冲突")
	ErrLeaseLost         = errors.New("任务租约已失效")
	ErrResourceExhausted = errors.New("任务资源超过上限")
)

type State string

const (
	StatePending      State = "pending"
	StateLeased       State = "leased"
	StateRunning      State = "running"
	StateRetryWait    State = "retry_wait"
	StateSucceeded    State = "succeeded"
	StateFailed       State = "failed"
	StateCancelled    State = "cancelled"
	StateInterrupted  State = "interrupted"
	StateManualReview State = "manual_review"
)

func (state State) Terminal() bool {
	return state == StateSucceeded || state == StateFailed || state == StateCancelled
}

type Task struct {
	ID                       string
	Type                     string
	State                    State
	Priority                 int
	PayloadVersion           int
	PayloadJSON              string
	IdempotencyKey           string
	WorkspaceID              string
	RequiredCapabilitiesJSON string
	CreatedAt                time.Time
	ScheduledAt              *time.Time
	StartedAt                *time.Time
	FinishedAt               *time.Time
	LeaseOwner               string
	LeaseExpiresAt           *time.Time
	AttemptCount             int
	MaxAttempts              int
	NextRetryAt              *time.Time
	CancelRequestedAt        *time.Time
	ResultSummaryJSON        string
	ErrorCode                string
	Revision                 int64
}

type CommandKind string

const (
	CommandLease            CommandKind = "lease"
	CommandRenewLease       CommandKind = "renew_lease"
	CommandStart            CommandKind = "start"
	CommandSucceed          CommandKind = "succeed"
	CommandFail             CommandKind = "fail"
	CommandRequestCancel    CommandKind = "request_cancel"
	CommandLeaseExpired     CommandKind = "lease_expired"
	CommandInterrupt        CommandKind = "interrupt"
	CommandRecover          CommandKind = "recover"
	CommandRetryDue         CommandKind = "retry_due"
	CommandManualRetry      CommandKind = "manual_retry"
	CommandReconcileSuccess CommandKind = "reconcile_success"
)

type Command struct {
	ExpectedRevision int64
	Kind             CommandKind
	Now              time.Time
	LeaseOwner       string
	LeaseUntil       time.Time
	Retryable        bool
	RetryAt          time.Time
	Resumable        bool
	ErrorCode        string
	ResultSummary    string
	ErrorSummary     string
}
