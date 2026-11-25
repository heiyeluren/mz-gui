package session

import (
	"crypto/subtle"
	"errors"
	"time"
)

const (
	EventLocked   EventKind = "locked"
	EventUnlocked EventKind = "unlocked"
)

var ErrSessionLocked = errors.New("会话已锁定")

func (store *SessionStore) Unlock(id, token string) error {
	if store == nil || id == "" || token == "" {
		return ErrInvalidSession
	}
	now := store.now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	session, exists := store.sessions[id]
	if !exists {
		return ErrInvalidSession
	}
	if !session.ExpiresAt.After(now) || !session.IssuedAt.Add(store.maxLifetime).After(now) {
		delete(store.sessions, id)
		store.publish(store.nextEventLocked(EventExpired, session, "ttl_expired"))
		return ErrSessionExpired
	}
	if len(token) != len(session.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(session.Token)) != 1 {
		return ErrInvalidSession
	}
	session.Locked = false
	session.LastSeenAt = now
	store.sessions[id] = session
	store.publish(store.nextEventLocked(EventUnlocked, session, "idle_unlock"))
	return nil
}

func (store *SessionStore) lockIfIdleLocked(session *Session, now time.Time) bool {
	if store.idleTimeout <= 0 || session.Locked {
		return session.Locked
	}
	lastSeen := session.LastSeenAt
	if lastSeen.IsZero() {
		lastSeen = session.IssuedAt
	}
	if now.Sub(lastSeen) > store.idleTimeout {
		session.Locked = true
		store.sessions[session.ID] = *session
		store.publish(store.nextEventLocked(EventLocked, *session, "idle_timeout"))
		return true
	}
	return false
}
