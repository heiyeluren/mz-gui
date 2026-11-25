package scheduler

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidArgument   = errors.New("调度参数无效")
	ErrNotFound          = errors.New("调度不存在")
	ErrConflict          = errors.New("调度发生冲突")
	ErrResourceExhausted = errors.New("调度资源超过上限")
)

type Kind string

const (
	KindOnce     Kind = "once"
	KindInterval Kind = "interval"
	KindCron     Kind = "cron"
)

type MisfirePolicy string

const (
	MisfireSkip           MisfirePolicy = "skip"
	MisfireRunOnce        MisfirePolicy = "run_once"
	MisfireCatchUpLimited MisfirePolicy = "catch_up_limited"
)

type OverlapPolicy string

const (
	OverlapForbid       OverlapPolicy = "forbid"
	OverlapAllowLimited OverlapPolicy = "allow_limited"
	OverlapReplace      OverlapPolicy = "replace"
)

type Schedule struct {
	ID               string
	Name             string
	Enabled          bool
	Kind             Kind
	Expression       string
	Timezone         string
	MisfirePolicy    MisfirePolicy
	OverlapPolicy    OverlapPolicy
	Jitter           time.Duration
	MaxCatchUp       int
	OverlapLimit     int
	NextRunAt        *time.Time
	LastRunAt        *time.Time
	TaskTemplateJSON string
	Revision         int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

type TaskTemplate struct {
	TaskType             string          `json:"task_type"`
	PayloadVersion       int             `json:"payload_version"`
	Payload              json.RawMessage `json:"payload"`
	Priority             int             `json:"priority"`
	WorkspaceID          string          `json:"workspace_id,omitempty"`
	RequiredCapabilities []string        `json:"required_capabilities,omitempty"`
	MaxAttempts          int             `json:"max_attempts"`
}

type PlannedOccurrence struct {
	PlannedAt   time.Time
	ScheduledAt time.Time
}

type Plan struct {
	Occurrences []PlannedOccurrence
	NextRunAt   *time.Time
}
