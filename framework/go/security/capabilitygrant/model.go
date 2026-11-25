// Package capabilitygrant 提供产品无关的高风险能力授权生命周期存储。
package capabilitygrant

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidArgument    = errors.New("Capability Grant 参数无效")
	ErrGrantAlreadyExists = errors.New("Capability Grant 已存在")
	ErrGrantNotFound      = errors.New("Capability Grant 不存在")
	ErrGrantInactive      = errors.New("Capability Grant 已失效")
	ErrRevisionConflict   = errors.New("Capability Grant 修订冲突")
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type Grant struct {
	ID         string
	Subject    string
	Capability string
	ScopeJSON  string
	RiskLevel  RiskLevel
	GrantedAt  time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	Source     string
	Revision   int64
}

func (grant Grant) Active(now time.Time) bool {
	return grant.RevokedAt == nil && (grant.ExpiresAt == nil || grant.ExpiresAt.After(now.UTC()))
}

type CreateRequest struct {
	ID         string
	Subject    string
	Capability string
	ScopeJSON  []byte
	RiskLevel  RiskLevel
	Source     string
	Now        time.Time
	ExpiresAt  *time.Time
}

type Query struct {
	Subject         string
	Capability      string
	Source          string
	IncludeInactive bool
	Now             time.Time
	Limit           int
}

func validateCreateRequest(request CreateRequest) (string, error) {
	if !identifierPattern.MatchString(request.ID) || !identifierPattern.MatchString(request.Subject) ||
		!identifierPattern.MatchString(request.Capability) || !identifierPattern.MatchString(request.Source) ||
		request.Now.IsZero() || !validRiskLevel(request.RiskLevel) {
		return "", ErrInvalidArgument
	}
	if request.ExpiresAt != nil && !request.ExpiresAt.After(request.Now) {
		return "", ErrInvalidArgument
	}
	scopeJSON, err := normalizeScopeJSON(request.ScopeJSON)
	if err != nil {
		return "", err
	}
	return scopeJSON, nil
}

func validateQuery(query Query) (int, error) {
	if query.Subject != "" && !identifierPattern.MatchString(query.Subject) {
		return 0, ErrInvalidArgument
	}
	if query.Capability != "" && !identifierPattern.MatchString(query.Capability) {
		return 0, ErrInvalidArgument
	}
	if query.Source != "" && !identifierPattern.MatchString(query.Source) {
		return 0, ErrInvalidArgument
	}
	if !query.IncludeInactive && query.Now.IsZero() {
		return 0, ErrInvalidArgument
	}
	if query.Limit == 0 {
		return 100, nil
	}
	if query.Limit < 1 || query.Limit > 500 {
		return 0, ErrInvalidArgument
	}
	return query.Limit, nil
}

func normalizeScopeJSON(scopeJSON []byte) (string, error) {
	var value map[string]any
	if len(scopeJSON) == 0 || json.Unmarshal(scopeJSON, &value) != nil || value == nil {
		return "", ErrInvalidArgument
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidArgument
	}
	return string(encoded), nil
}

func validRiskLevel(value RiskLevel) bool {
	switch value {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
		return true
	default:
		return false
	}
}
