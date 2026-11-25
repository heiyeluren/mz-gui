package remoteservice

import (
	"crypto/x509"
	"encoding/json"
	"strings"
	"time"
)

type TLSPinDocument struct {
	Version       string    `json:"version"`
	NotBefore     time.Time `json:"not_before"`
	NextActiveAt  time.Time `json:"next_active_at,omitempty"`
	NotAfter      time.Time `json:"not_after"`
	CurrentPins   []string  `json:"current_pins"`
	NextPins      []string  `json:"next_pins,omitempty"`
	PreviousPins  []string  `json:"previous_pins,omitempty"`
}

type TLSPinPolicy struct {
	document TLSPinDocument
	current  TLSPinSet
	next     TLSPinSet
	previous TLSPinSet
}

func ParseTLSPinDocument(raw []byte) (TLSPinDocument, error) {
	var document TLSPinDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return TLSPinDocument{}, ErrInvalidConfig
	}
	return document, nil
}

func NewTLSPinPolicy(document TLSPinDocument) (*TLSPinPolicy, error) {
	if strings.TrimSpace(document.Version) == "" || document.NotBefore.IsZero() || document.NotAfter.IsZero() || !document.NotAfter.After(document.NotBefore) {
		return nil, ErrInvalidConfig
	}
	current, err := NewTLSPinSet(document.CurrentPins)
	if err != nil {
		return nil, err
	}
	policy := &TLSPinPolicy{document: cloneTLSPinDocument(document), current: current}
	if len(document.NextPins) > 0 {
		next, err := NewTLSPinSet(document.NextPins)
		if err != nil {
			return nil, err
		}
		if document.NextActiveAt.IsZero() {
			return nil, ErrInvalidConfig
		}
		policy.next = next
	}
	if len(document.PreviousPins) > 0 {
		previous, err := NewTLSPinSet(document.PreviousPins)
		if err != nil {
			return nil, err
		}
		policy.previous = previous
	}
	return policy, nil
}

func (policy *TLSPinPolicy) Document() TLSPinDocument {
	if policy == nil {
		return TLSPinDocument{}
	}
	return cloneTLSPinDocument(policy.document)
}

func (policy *TLSPinPolicy) PinSetAt(at time.Time) (TLSPinSet, error) {
	if policy == nil {
		return TLSPinSet{}, ErrInvalidConfig
	}
	if at.Before(policy.document.NotBefore) {
		return TLSPinSet{}, ErrTLSPinPolicyNotActive
	}
	if !at.Before(policy.document.NotAfter) {
		return TLSPinSet{}, ErrTLSPinPolicyExpired
	}
	values := append([]string{}, policy.document.CurrentPins...)
	if !policy.next.Empty() && !at.Before(policy.document.NextActiveAt) {
		values = append(values, policy.document.NextPins...)
	}
	if !policy.previous.Empty() && (policy.next.Empty() || at.Before(policy.document.NextActiveAt)) {
		values = append(values, policy.document.PreviousPins...)
	}
	return NewTLSPinSet(uniquePins(values))
}

func (policy *TLSPinPolicy) VerifyPeerCertificates(at time.Time, rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	pins, err := policy.PinSetAt(at)
	if err != nil {
		return err
	}
	return pins.VerifyPeerCertificates(rawCerts, verifiedChains)
}

func (policy *TLSPinPolicy) Rotate(at time.Time) (*TLSPinPolicy, error) {
	if policy == nil || policy.next.Empty() {
		return nil, ErrInvalidConfig
	}
	if at.Before(policy.document.NextActiveAt) {
		return nil, ErrTLSPinPolicyNotActive
	}
	if !at.Before(policy.document.NotAfter) {
		return nil, ErrTLSPinPolicyExpired
	}
	rotated := TLSPinDocument{
		Version:      policy.document.Version,
		NotBefore:    at,
		NotAfter:     policy.document.NotAfter,
		CurrentPins:  append([]string{}, policy.document.NextPins...),
		PreviousPins: append([]string{}, policy.document.CurrentPins...),
	}
	return NewTLSPinPolicy(rotated)
}

func uniquePins(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneTLSPinDocument(document TLSPinDocument) TLSPinDocument {
	cloned := document
	cloned.CurrentPins = append([]string{}, document.CurrentPins...)
	cloned.NextPins = append([]string{}, document.NextPins...)
	cloned.PreviousPins = append([]string{}, document.PreviousPins...)
	return cloned
}
