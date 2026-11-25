package health

import (
	"errors"
	"regexp"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/core/model"
)

const SchemaVersion = 1

const (
	ReasonNotObserved = "core.not_observed"
	ReasonOK          = "core.ok"
)

var (
	ErrInvalidCatalog      = errors.New("Framework 健康检查 Catalog 无效")
	ErrInvalidDefinition   = errors.New("Framework 健康检查定义无效")
	ErrInvalidCheck        = errors.New("Framework 健康检查观测无效")
	ErrInvalidRegistry     = errors.New("Framework 健康检查 Registry 无效")
	ErrCheckNotRegistered  = errors.New("Framework 健康检查未注册")
	ErrReasonNotRegistered = errors.New("Framework 健康检查原因未注册")
	ErrKindMismatch        = errors.New("Framework 健康检查类型不匹配")
	ErrStaleObservation    = errors.New("Framework 健康检查观测已过期")
	ErrObservationConflict = errors.New("Framework 健康检查观测冲突")

	checkIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_-]*)+$`)
	reasonCodePattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`)
	descriptionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`)
	ownerPattern          = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type Kind string

const (
	KindLiveness   Kind = "liveness"
	KindReadiness  Kind = "readiness"
	KindDependency Kind = "dependency"
)

func (kind Kind) Valid() bool {
	return kind == KindLiveness || kind == KindReadiness || kind == KindDependency
}

type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown"
)

func (status Status) Valid() bool {
	return status == StatusHealthy || status == StatusDegraded || status == StatusUnhealthy || status == StatusUnknown
}

type Definition struct {
	CheckID        string   `json:"check_id"`
	Kind           Kind     `json:"kind"`
	Required       bool     `json:"required"`
	DescriptionKey string   `json:"description_key"`
	Owner          string   `json:"owner"`
	ReasonCodes    []string `json:"reason_codes"`
}

func (definition Definition) Validate() error {
	if !checkIDPattern.MatchString(definition.CheckID) || !definition.Kind.Valid() ||
		!descriptionKeyPattern.MatchString(definition.DescriptionKey) || !ownerPattern.MatchString(definition.Owner) ||
		len(definition.ReasonCodes) == 0 {
		return ErrInvalidDefinition
	}
	if _, err := model.ParseID(definition.CheckID); err != nil {
		return ErrInvalidDefinition
	}
	seenReasons := make(map[string]struct{}, len(definition.ReasonCodes))
	hasInitialReason := false
	for _, reason := range definition.ReasonCodes {
		if !reasonCodePattern.MatchString(reason) {
			return ErrInvalidDefinition
		}
		if _, exists := seenReasons[reason]; exists {
			return ErrInvalidDefinition
		}
		seenReasons[reason] = struct{}{}
		hasInitialReason = hasInitialReason || reason == ReasonNotObserved
	}
	if !hasInitialReason {
		return ErrInvalidDefinition
	}
	return nil
}

func (definition Definition) allowsReason(reason string) bool {
	for _, allowed := range definition.ReasonCodes {
		if allowed == reason {
			return true
		}
	}
	return false
}

type Check struct {
	CheckID    model.ID        `json:"check_id"`
	Kind       Kind            `json:"kind"`
	Status     Status          `json:"status"`
	ReasonCode string          `json:"reason_code"`
	ObservedAt model.Timestamp `json:"observed_at"`
}

func (check Check) Validate() error {
	if !checkIDPattern.MatchString(check.CheckID.String()) || !check.Kind.Valid() || !check.Status.Valid() ||
		strings.TrimSpace(check.ReasonCode) != check.ReasonCode || !reasonCodePattern.MatchString(check.ReasonCode) {
		return ErrInvalidCheck
	}
	if _, err := model.ParseID(check.CheckID.String()); err != nil {
		return ErrInvalidCheck
	}
	if _, err := model.ParseTimestamp(string(check.ObservedAt)); err != nil {
		return ErrInvalidCheck
	}
	if (check.Status == StatusHealthy) != (check.ReasonCode == ReasonOK) ||
		(check.Status == StatusUnknown && check.ReasonCode == ReasonOK) ||
		(check.Status != StatusUnknown && check.ReasonCode == ReasonNotObserved) {
		return ErrInvalidCheck
	}
	return nil
}

type Report struct {
	Status     Status          `json:"status"`
	Checks     []Check         `json:"checks"`
	ObservedAt model.Timestamp `json:"observed_at"`
	Build      model.Version   `json:"build"`
}
