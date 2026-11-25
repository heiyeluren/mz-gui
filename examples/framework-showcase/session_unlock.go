package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/security/session"
)

const (
	showcaseSessionID    = "showcase-shell-session"
	showcaseSessionToken = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func unlockShowcaseShellSession() error {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	store, err := session.NewSessionStore(session.StoreOptions{
		TTL:         15 * time.Minute,
		IdleTimeout: 5 * time.Minute,
		MaxSessions: 1,
		Now:         func() time.Time { return now },
		NewID:       func() (string, error) { return showcaseSessionID, nil },
		NewToken:    func() (string, error) { return showcaseSessionToken, nil },
	})
	if err != nil {
		return err
	}
	issued, err := store.Issue("showcase-shell", "1.0.0", nil)
	if err != nil {
		return err
	}
	now = now.Add(6 * time.Minute)
	if _, err := store.ValidateCredential(issued.ID, issued.Token); !errors.Is(err, session.ErrSessionLocked) {
		if err == nil {
			return fmt.Errorf("showcase 空闲锁定未生效")
		}
		return err
	}
	return store.Unlock(issued.ID, issued.Token)
}
