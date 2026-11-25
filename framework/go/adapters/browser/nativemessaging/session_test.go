package nativemessaging

import (
	"errors"
	"testing"
	"time"
)

func TestNegotiateSelectsProtocolAndCapabilities(t *testing.T) {
	result, err := Negotiate(NegotiationPolicy{
		Protocol:            1,
		AllowedCapabilities: []string{"browser.tab.control", "browser.tab.read"},
	}, NegotiationRequest{
		ProtocolMin:  1,
		ProtocolMax:  2,
		Capabilities: []string{"browser.tab.read", "browser.tab.control"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Protocol != 1 || len(result.Capabilities) != 2 || result.Capabilities[0] != "browser.tab.control" || result.Capabilities[1] != "browser.tab.read" {
		t.Fatalf("negotiation=%#v", result)
	}
}

func TestNegotiateRejectsIncompatibleOrEscalatedClient(t *testing.T) {
	policy := NegotiationPolicy{Protocol: 1, AllowedCapabilities: []string{"browser.tab.read"}}
	if _, err := Negotiate(policy, NegotiationRequest{ProtocolMin: 2, ProtocolMax: 3}); !errors.Is(err, ErrProtocolIncompatible) {
		t.Fatalf("protocol error=%v", err)
	}
	if _, err := Negotiate(policy, NegotiationRequest{ProtocolMin: 1, ProtocolMax: 1, Capabilities: []string{"terminal.execute"}}); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("capability error=%v", err)
	}
}

func TestSessionLeaseValidatesAndRenews(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	lease, err := NewSessionLease("session-test-1234", now, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !lease.Valid("session-test-1234", now.Add(14*time.Minute)) || lease.Valid("session-wrong-1234", now) || lease.Valid("session-test-1234", now.Add(15*time.Minute)) {
		t.Fatalf("lease validation failed: %#v", lease)
	}
	renewed, err := lease.Renew("session-test-1234", now.Add(14*time.Minute))
	if err != nil || !renewed.Valid("session-test-1234", now.Add(28*time.Minute)) || renewed.ExpiresAt() != now.Add(29*time.Minute) {
		t.Fatalf("renewed=%#v err=%v", renewed, err)
	}
}
