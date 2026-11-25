package managedprocess

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	defaultReadinessTimeout  = 15 * time.Second
	defaultReadinessInterval = 100 * time.Millisecond
	defaultProbeTimeout      = 2 * time.Second
	defaultHeartbeatInterval = 5 * time.Second
	defaultFailureThreshold  = 3
)

var (
	ErrReadinessFailed    = errors.New("受管进程就绪检查失败")
	ErrIncompatible       = errors.New("受管进程协议或制品不兼容")
	ErrTerminationTimeout = errors.New("受管进程终止超时")

	probeIdentityPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	probeCapabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z][a-z0-9-]*)+$`)
)

type ProbePhase string

const (
	ProbePhaseReadiness ProbePhase = "readiness"
	ProbePhaseHeartbeat ProbePhase = "heartbeat"
)

func (phase ProbePhase) Valid() bool {
	return phase == ProbePhaseReadiness || phase == ProbePhaseHeartbeat
}

type ProbeStatus string

const (
	ProbeStatusReady        ProbeStatus = "ready"
	ProbeStatusDegraded     ProbeStatus = "degraded"
	ProbeStatusNotReady     ProbeStatus = "not_ready"
	ProbeStatusIncompatible ProbeStatus = "incompatible"
)

func (status ProbeStatus) Valid() bool {
	return status == ProbeStatusReady || status == ProbeStatusDegraded || status == ProbeStatusNotReady || status == ProbeStatusIncompatible
}

type ProbeRequest struct {
	ProcessID string     `json:"process_id"`
	PID       int        `json:"pid"`
	Attempt   uint64     `json:"attempt"`
	Phase     ProbePhase `json:"phase"`
}

type ProbeResult struct {
	Status         ProbeStatus `json:"status"`
	ReasonCode     string      `json:"reason_code"`
	ProcessVersion string      `json:"process_version,omitempty"`
	ProtocolMajor  uint32      `json:"protocol_major,omitempty"`
	ProtocolMinor  uint32      `json:"protocol_minor,omitempty"`
	InstanceID     string      `json:"instance_id,omitempty"`
	Capabilities   []string    `json:"capabilities,omitempty"`
}

func (result ProbeResult) valid() bool {
	if !result.Status.Valid() || !reasonCodePattern.MatchString(result.ReasonCode) ||
		strings.TrimSpace(result.ProcessVersion) != result.ProcessVersion || len(result.ProcessVersion) > 128 ||
		strings.TrimSpace(result.InstanceID) != result.InstanceID || len(result.Capabilities) > 64 {
		return false
	}
	if result.InstanceID != "" && !probeIdentityPattern.MatchString(result.InstanceID) {
		return false
	}
	seen := make(map[string]struct{}, len(result.Capabilities))
	for _, capability := range result.Capabilities {
		if !probeCapabilityPattern.MatchString(capability) {
			return false
		}
		if _, exists := seen[capability]; exists {
			return false
		}
		seen[capability] = struct{}{}
	}
	return true
}

func (result ProbeResult) clone() ProbeResult {
	result.Capabilities = append([]string(nil), result.Capabilities...)
	return result
}

type Probe interface {
	Check(context.Context, ProbeRequest) (ProbeResult, error)
}

type ProbeFunc func(context.Context, ProbeRequest) (ProbeResult, error)

func (probe ProbeFunc) Check(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	return probe(ctx, request)
}

type ProbeConfig struct {
	// MinFailureDuration 与连续失败次数同时满足才停止；零保留原次数策略。
	MinFailureDuration time.Duration `json:"min_failure_duration"`
	Probe              Probe         `json:"-"`
	ReadinessTimeout   time.Duration `json:"readiness_timeout"`
	ReadinessInterval  time.Duration `json:"readiness_interval"`
	ProbeTimeout       time.Duration `json:"probe_timeout"`
	HeartbeatInterval  time.Duration `json:"heartbeat_interval"`
	FailureThreshold   int           `json:"failure_threshold"`
}

func normalizeProbe(value ProbeConfig) (ProbeConfig, error) {
	if value.Probe == nil {
		if value.ReadinessTimeout != 0 || value.ReadinessInterval != 0 || value.ProbeTimeout != 0 ||
			value.HeartbeatInterval != 0 || value.FailureThreshold != 0 || value.MinFailureDuration != 0 {
			return ProbeConfig{}, ErrInvalidSpec
		}
		return ProbeConfig{}, nil
	}
	if value.MinFailureDuration < 0 || value.MinFailureDuration > 5*time.Minute || value.ReadinessTimeout < 0 || value.ReadinessInterval < 0 || value.ProbeTimeout < 0 ||
		value.HeartbeatInterval < 0 || value.FailureThreshold < 0 {
		return ProbeConfig{}, ErrInvalidSpec
	}
	if value.ReadinessTimeout == 0 {
		value.ReadinessTimeout = defaultReadinessTimeout
	}
	if value.ReadinessInterval == 0 {
		value.ReadinessInterval = defaultReadinessInterval
	}
	if value.ProbeTimeout == 0 {
		value.ProbeTimeout = defaultProbeTimeout
	}
	if value.HeartbeatInterval == 0 {
		value.HeartbeatInterval = defaultHeartbeatInterval
	}
	if value.FailureThreshold == 0 {
		value.FailureThreshold = defaultFailureThreshold
	}
	if value.ReadinessTimeout > 5*time.Minute || value.ReadinessInterval > time.Minute ||
		value.ProbeTimeout > time.Minute || value.HeartbeatInterval > 10*time.Minute || value.FailureThreshold > 100 ||
		value.ReadinessTimeout < 10*time.Millisecond || value.ReadinessInterval < 10*time.Millisecond ||
		value.ProbeTimeout < time.Millisecond || value.HeartbeatInterval < 50*time.Millisecond ||
		value.ProbeTimeout > value.ReadinessTimeout || value.ReadinessInterval > value.ReadinessTimeout {
		return ProbeConfig{}, ErrInvalidSpec
	}
	return value, nil
}

func checkProbe(ctx context.Context, probe Probe, request ProbeRequest) (result ProbeResult, err error) {
	if ctx == nil || probe == nil || !request.Phase.Valid() || !processID.MatchString(request.ProcessID) || request.PID < 1 || request.Attempt < 1 {
		return ProbeResult{}, ErrInvalidSpec
	}
	defer func() {
		if recover() != nil {
			result = ProbeResult{Status: ProbeStatusNotReady, ReasonCode: ReasonProbePanic}
			err = ErrReadinessFailed
		}
	}()
	result, err = probe.Check(ctx, request)
	result = result.clone()
	if !result.valid() {
		return ProbeResult{Status: ProbeStatusNotReady, ReasonCode: ReasonProbeInvalid}, errors.Join(ErrReadinessFailed, err)
	}
	return result, err
}
