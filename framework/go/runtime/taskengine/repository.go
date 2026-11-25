package taskengine

import (
	"context"
	"encoding/json"
	"time"
)

const (
	MaxPayloadBytes     = 1 << 20
	MaxOutputChunkBytes = 64 << 10
	MaxCheckpointBytes  = 48 << 10
	MaxResultBytes      = 64 << 10
	MaxAttempts         = 10
	MaxOutputPage       = 1000
	MaxTaskPage         = 100
)

type CreateRequest struct {
	Type                 string
	Priority             int
	PayloadVersion       int
	Payload              json.RawMessage
	IdempotencyKey       string
	WorkspaceID          string
	RequiredCapabilities []string
	ScheduledAt          *time.Time
	MaxAttempts          int
	Now                  time.Time
}

type LeaseRequest struct {
	Owner    string
	Now      time.Time
	Duration time.Duration
}

type Attempt struct {
	ID                string
	TaskID            string
	Number            int
	WorkerID          string
	StartedAt         time.Time
	FinishedAt        *time.Time
	Outcome           string
	ErrorCode         string
	ErrorSummary      string
	OutputStartCursor int64
	OutputEndCursor   int64
}

type Output struct {
	TaskID     string
	Cursor     int64
	Kind       string
	OccurredAt time.Time
	Payload    []byte
}

type CheckpointRequest struct {
	TaskID    string
	AttemptNo int
	Key       string
	Payload   json.RawMessage
	Now       time.Time
}

type Checkpoint struct {
	TaskID       string
	Key          string
	Revision     int64
	AttemptNo    int
	OutputCursor int64
	PayloadJSON  string
	CreatedAt    time.Time
}

type ListRequest struct {
	States          []State
	Types           []string
	WorkspaceID     string
	BeforeCreatedAt *time.Time
	BeforeTaskID    string
	Limit           int
}

type TaskPage struct {
	Tasks         []Task
	HasMore       bool
	NextCreatedAt *time.Time
	NextTaskID    string
}

type Repository interface {
	Create(context.Context, CreateRequest) (Task, bool, error)
	Get(context.Context, string) (Task, error)
	List(context.Context, ListRequest) (TaskPage, error)
	Retry(context.Context, string, time.Time) (Task, error)
	RetryIdempotent(context.Context, string, string, string, time.Time) (Task, bool, error)
	LeaseNext(context.Context, LeaseRequest) (Task, bool, error)
	Start(context.Context, string, string, time.Time) (Task, Attempt, error)
	RenewLease(context.Context, string, string, time.Time, time.Duration) (Task, error)
	RequestCancel(context.Context, string, time.Time) (Task, error)
	RequestCancelIdempotent(context.Context, string, string, string, time.Time) (Task, bool, error)
	Finish(context.Context, string, Command) (Task, error)
	AppendOutput(context.Context, string, string, []byte, time.Time) (Output, error)
	ReadOutput(context.Context, string, int64, int) ([]Output, error)
	SaveCheckpoint(context.Context, CheckpointRequest) (Checkpoint, bool, error)
	LatestCheckpoint(context.Context, string) (Checkpoint, bool, error)
	PromoteDueRetries(context.Context, time.Time, int) (int, error)
	RecoverExpired(context.Context, time.Time, int, RecoveryPolicy) (int, error)
}

type RecoveryPolicy func(Task) bool
