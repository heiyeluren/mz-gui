// Package nativee2e 提供可复用的原生桌面进程、健康、RPC 与路由验证基座。
package nativee2e

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidConfig       = errors.New("原生 E2E 配置无效")
	ErrRouteDriverRequired = errors.New("正式路由验证必须配置真实 UI 驱动")
	ErrValidationFailed    = errors.New("原生 E2E 验证失败")
)

type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

type Command struct {
	Executable string            `json:"executable"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Dir        string            `json:"dir,omitempty"`
}

type Probe interface {
	Name() string
	Check(context.Context) (string, error)
}

type RouteDriver interface {
	Visit(context.Context, string) (string, error)
}

// RouteEvidence 由真实平台 UI 驱动返回，Framework 会核对请求与实际观测路由。
type RouteEvidence struct {
	SchemaVersion    int    `json:"schema_version"`
	RequestedRouteID string `json:"requested_route_id"`
	ObservedRouteID  string `json:"observed_route_id"`
	Status           Status `json:"status"`
	Detail           string `json:"detail,omitempty"`
}

type Config struct {
	Artifact         string
	ArtifactArgs     []string
	ArtifactEnv      map[string]string
	WorkingDir       string
	TargetOS         string
	DesktopName      string
	DesktopStability time.Duration
	StartupTimeout   time.Duration
	ProbeInterval    time.Duration
	HealthProbes     []Probe
	RPCProbes        []Probe
	Routes           []string
	RouteDriver      RouteDriver
	UIAutomation     UIAutomation
	Now              func() time.Time
}

type UIAutomation struct {
	Mode       string `json:"mode"`
	Configured bool   `json:"configured"`
	Remaining  string `json:"remaining,omitempty"`
}

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	RunID         string        `json:"run_id"`
	Status        Status        `json:"status"`
	Platform      string        `json:"platform"`
	Architecture  string        `json:"architecture"`
	Artifact      string        `json:"artifact"`
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	DurationMS    int64         `json:"duration_ms"`
	Desktop       ProcessResult `json:"desktop"`
	HealthChecks  []CheckResult `json:"health_checks"`
	RPCChecks     []CheckResult `json:"rpc_checks"`
	Routes        []RouteResult `json:"routes"`
	UIAutomation  UIAutomation  `json:"ui_automation"`
	FailureCount  int           `json:"failure_count"`
	Failures      []string      `json:"failures,omitempty"`
	ProcessOutput ProcessOutput `json:"process_output"`
}

type ProcessResult struct {
	Name             string `json:"name"`
	PID              int    `json:"pid"`
	Executable       string `json:"executable"`
	Status           Status `json:"status"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	StoppedByHarness bool   `json:"stopped_by_harness"`
}

type ProcessOutput struct {
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Truncated bool   `json:"truncated"`
}

type CheckResult struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	Status     Status    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	Detail     string    `json:"detail,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type RouteResult struct {
	RouteID    string    `json:"route_id"`
	Status     Status    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	Detail     string    `json:"detail,omitempty"`
	Error      string    `json:"error,omitempty"`
}
