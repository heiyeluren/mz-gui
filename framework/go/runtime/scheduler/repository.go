package scheduler

import (
	"context"
	"encoding/json"
	"time"
)

const MaxSchedulePage = 100

type CreateRequest struct {
	Name          string
	Enabled       bool
	Kind          Kind
	Expression    string
	Timezone      string
	MisfirePolicy MisfirePolicy
	OverlapPolicy OverlapPolicy
	Jitter        time.Duration
	MaxCatchUp    int
	OverlapLimit  int
	TaskTemplate  json.RawMessage
	Now           time.Time
}

type UpdateRequest struct {
	ID               string
	ExpectedRevision int64
	CreateRequest
}

type ListRequest struct {
	IncludeDisabled bool
	Limit           int
}

type OccurrenceListRequest struct {
	ScheduleID         string
	BeforePlannedAt    *time.Time
	BeforeOccurrenceID string
	Limit              int
}

type OccurrencePage struct {
	Occurrences      []Occurrence
	HasMore          bool
	NextPlannedAt    *time.Time
	NextOccurrenceID string
}

type DeleteResult struct {
	ScheduleID string
	Revision   int64
}

type RunNowResult struct {
	Schedule   Schedule
	Occurrence Occurrence
}

type Occurrence struct {
	ID         string
	ScheduleID string
	PlannedAt  time.Time
	TaskID     string
	State      string
	ReasonCode string
	CreatedAt  time.Time
}

type Repository interface {
	Create(context.Context, CreateRequest) (Schedule, error)
	Get(context.Context, string) (Schedule, error)
	List(context.Context, ListRequest) ([]Schedule, error)
	Update(context.Context, UpdateRequest) (Schedule, error)
	SetEnabled(context.Context, string, int64, bool, time.Time) (Schedule, error)
	Delete(context.Context, string, int64, time.Time) error
	Due(context.Context, time.Time, int) ([]Schedule, error)
	ApplyPlan(context.Context, Schedule, Plan, time.Time) ([]Occurrence, error)
}

type RPCRepository interface {
	CreateIdempotent(context.Context, CreateRequest, string) (Schedule, bool, error)
	Get(context.Context, string) (Schedule, error)
	List(context.Context, ListRequest) ([]Schedule, error)
	UpdateIdempotent(context.Context, UpdateRequest, string) (Schedule, bool, error)
	SetEnabledIdempotent(context.Context, string, int64, bool, string, time.Time) (Schedule, bool, error)
	DeleteIdempotent(context.Context, string, int64, string, time.Time) (DeleteResult, bool, error)
	RunNowIdempotent(context.Context, string, int64, string, time.Time) (RunNowResult, bool, error)
	ListOccurrences(context.Context, OccurrenceListRequest) (OccurrencePage, error)
}
