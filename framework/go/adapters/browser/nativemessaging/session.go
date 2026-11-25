package nativemessaging

import (
	"crypto/subtle"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidNegotiation   = errors.New("Native Messaging 协商参数无效")
	ErrProtocolIncompatible = errors.New("Native Messaging 协议版本不兼容")
	ErrCapabilityDenied     = errors.New("Native Messaging capability 未授权")
	ErrSessionInvalid       = errors.New("Native Messaging 会话无效")
)

type NegotiationPolicy struct {
	Protocol            uint32
	AllowedCapabilities []string
}

type NegotiationRequest struct {
	ProtocolMin  uint32
	ProtocolMax  uint32
	Capabilities []string
}

type Negotiation struct {
	Protocol     uint32
	Capabilities []string
}

func Negotiate(policy NegotiationPolicy, request NegotiationRequest) (Negotiation, error) {
	if policy.Protocol == 0 || request.ProtocolMin == 0 || request.ProtocolMax < request.ProtocolMin {
		return Negotiation{}, ErrInvalidNegotiation
	}
	if policy.Protocol < request.ProtocolMin || policy.Protocol > request.ProtocolMax {
		return Negotiation{}, ErrProtocolIncompatible
	}
	allowed := make(map[string]struct{}, len(policy.AllowedCapabilities))
	for _, capability := range policy.AllowedCapabilities {
		if capability = strings.TrimSpace(capability); capability == "" {
			return Negotiation{}, ErrInvalidNegotiation
		}
		allowed[capability] = struct{}{}
	}
	seen := make(map[string]struct{}, len(request.Capabilities))
	capabilities := make([]string, 0, len(request.Capabilities))
	for _, capability := range request.Capabilities {
		if _, ok := allowed[capability]; !ok {
			return Negotiation{}, ErrCapabilityDenied
		}
		if _, duplicate := seen[capability]; duplicate {
			return Negotiation{}, ErrInvalidNegotiation
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	return Negotiation{Protocol: policy.Protocol, Capabilities: capabilities}, nil
}

type SessionLease struct {
	id        string
	expiresAt time.Time
	ttl       time.Duration
}

func NewSessionLease(id string, now time.Time, ttl time.Duration) (SessionLease, error) {
	if !validSessionID(id) || now.IsZero() || ttl < time.Second || ttl > 24*time.Hour {
		return SessionLease{}, ErrSessionInvalid
	}
	return SessionLease{id: id, expiresAt: now.UTC().Add(ttl), ttl: ttl}, nil
}

func (lease SessionLease) Valid(id string, now time.Time) bool {
	if !validSessionID(id) || now.IsZero() || lease.expiresAt.IsZero() || lease.ttl <= 0 || len(id) != len(lease.id) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(id), []byte(lease.id)) == 1 && now.UTC().Before(lease.expiresAt)
}

func (lease SessionLease) Renew(id string, now time.Time) (SessionLease, error) {
	if !lease.Valid(id, now) {
		return SessionLease{}, ErrSessionInvalid
	}
	lease.expiresAt = now.UTC().Add(lease.ttl)
	return lease, nil
}

func (lease SessionLease) ExpiresAt() time.Time {
	return lease.expiresAt
}

func validSessionID(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}
