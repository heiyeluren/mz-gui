package session

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSessionStoreLocksAfterIdleTimeoutAndUnlocksWithToken(t *testing.T) {
	now := time.Date(2026, 8, 16, 21, 0, 0, 0, time.UTC)
	store, err := NewSessionStore(StoreOptions{
		TTL: 15 * time.Minute, IdleTimeout: 5 * time.Minute, MaxSessions: 2, Now: func() time.Time { return now },
		NewID:    func() (string, error) { return "session-idle", nil },
		NewToken: func() (string, error) { return fmt.Sprintf("%064s", "token"), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.Issue("desktop", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := store.ValidateCredential(issued.ID, issued.Token); !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("空闲超时必须锁定: %v", err)
	}
	if err := store.Unlock(issued.ID, issued.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateCredential(issued.ID, issued.Token); err != nil {
		t.Fatalf("解锁后应通过: %v", err)
	}
}

func TestSessionStoreRejectsInvalidIdleTimeout(t *testing.T) {
	if _, err := NewSessionStore(StoreOptions{TTL: 15 * time.Minute, IdleTimeout: 30 * time.Minute, MaxSessions: 1}); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("空闲超时超过 TTL 必须拒绝: %v", err)
	}
}

func TestSessionStoreRenewCredentialRefusesIdleLockedSession(t *testing.T) {
	now := time.Date(2026, 8, 16, 21, 0, 0, 0, time.UTC)
	sequence := 0
	store, err := NewSessionStore(StoreOptions{
		TTL: 15 * time.Minute, IdleTimeout: 5 * time.Minute, MaxSessions: 2, Now: func() time.Time { return now },
		NewID: func() (string, error) { return "session-idle-renew", nil },
		NewToken: func() (string, error) {
			sequence++
			return fmt.Sprintf("%064d", sequence), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.Issue("desktop", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := store.RenewCredential(issued.ID, issued.Token); !errors.Is(err, ErrSessionLocked) {
		t.Fatalf("空闲锁定后不得续签: %v", err)
	}
	if err := store.Unlock(issued.ID, issued.Token); err != nil {
		t.Fatal(err)
	}
	renewed, err := store.RenewCredential(issued.ID, issued.Token)
	if err != nil {
		t.Fatalf("解锁后应可续签: %v", err)
	}
	if renewed.Token == issued.Token {
		t.Fatal("解锁后续签必须轮换 token")
	}
}
