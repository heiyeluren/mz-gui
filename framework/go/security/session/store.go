package session

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/eventbus"
)

var (
	ErrInvalidSession    = errors.New("会话无效")
	ErrSessionExpired    = errors.New("会话已过期")
	ErrResourceExhausted = errors.New("会话数量超过上限")
)

type IDSource func() (string, error)

type StoreOptions struct {
	TTL                 time.Duration
	IdleTimeout         time.Duration
	MaxLifetime         time.Duration
	MaxSessions         int
	EventBuffer         int
	MaxEventSubscribers int
	Now                 func() time.Time
	NewID               IDSource
	NewToken            IDSource
}

type Session struct {
	ID               string
	Token            string
	Component        string
	ComponentVersion string
	IssuedAt         time.Time
	LastSeenAt       time.Time
	ExpiresAt        time.Time
	Locked           bool
	capabilities     map[string]struct{}
}

type EventKind string

const (
	EventIssued  EventKind = "issued"
	EventRenewed EventKind = "renewed"
	EventRevoked EventKind = "revoked"
	EventExpired EventKind = "expired"

	RevokeReasonUnspecified    = "unspecified"
	RevokeReasonClientShutdown = "client_shutdown"
	RevokeReasonReplaced       = "replaced"
	RevokeReasonPolicyChanged  = "policy_changed"
)

type Event struct {
	Sequence         uint64
	Kind             EventKind
	SessionID        string
	Component        string
	ComponentVersion string
	OccurredAt       time.Time
	ExpiresAt        time.Time
	Reason           string
}

func (session Session) Has(capability string) bool {
	_, exists := session.capabilities[capability]
	return exists
}

func (session Session) Capabilities() []string {
	values := make([]string, 0, len(session.capabilities))
	for capability := range session.capabilities {
		values = append(values, capability)
	}
	sort.Strings(values)
	return values
}

type SessionStore struct {
	mu          sync.Mutex
	ttl         time.Duration
	idleTimeout time.Duration
	maxLifetime time.Duration
	maxSessions int
	now         func() time.Time
	newID       IDSource
	newToken    IDSource
	sessions    map[string]Session
	events      *eventbus.Bus[Event]
	sequence    uint64
}

func NewSessionStore(options StoreOptions) (*SessionStore, error) {
	if options.TTL < time.Minute || options.TTL > 24*time.Hour || options.MaxSessions < 1 || options.MaxSessions > 4096 ||
		options.MaxLifetime < 0 || options.EventBuffer < 0 || options.MaxEventSubscribers < 0 ||
		options.IdleTimeout < 0 || (options.IdleTimeout > 0 && (options.IdleTimeout < time.Minute || options.IdleTimeout > options.TTL)) {
		return nil, fmt.Errorf("%w: 会话存储选项无效", ErrInvalidSession)
	}
	if options.MaxLifetime == 0 {
		options.MaxLifetime = 8 * options.TTL
		if options.MaxLifetime > 7*24*time.Hour {
			options.MaxLifetime = 7 * 24 * time.Hour
		}
	}
	if options.MaxLifetime < options.TTL || options.MaxLifetime > 7*24*time.Hour {
		return nil, fmt.Errorf("%w: 会话绝对期限无效", ErrInvalidSession)
	}
	if options.EventBuffer == 0 {
		options.EventBuffer = 64
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = randomSessionID
	}
	if options.NewToken == nil {
		options.NewToken = randomSessionID
	}
	events, err := eventbus.New[Event](eventbus.Options{Buffer: options.EventBuffer, MaxSubscribers: options.MaxEventSubscribers})
	if err != nil {
		return nil, fmt.Errorf("%w: 会话事件总线无效", ErrInvalidSession)
	}
	return &SessionStore{
		ttl: options.TTL, idleTimeout: options.IdleTimeout, maxLifetime: options.MaxLifetime, maxSessions: options.MaxSessions, now: options.Now,
		newID: options.NewID, newToken: options.NewToken, sessions: make(map[string]Session), events: events,
	}, nil
}

func (store *SessionStore) SubscribeEvents() (*eventbus.Subscription[Event], error) {
	if store == nil || store.events == nil {
		return nil, ErrInvalidSession
	}
	return store.events.Subscribe()
}

// MaxCapabilities 是单个会话或进程角色可持有的能力项上限。
// 它只是防止握手携带超大列表的防御性约束；角色策略、握手授予与客户端探针共用此值。
const MaxCapabilities = 128

func (store *SessionStore) Issue(component, componentVersion string, capabilities []string) (Session, error) {
	if component == "" || componentVersion == "" || len(component) > 64 || len(componentVersion) > 64 || len(capabilities) > MaxCapabilities {
		return Session{}, ErrInvalidSession
	}
	now := store.now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.removeExpired(now)
	if len(store.sessions) >= store.maxSessions {
		return Session{}, ErrResourceExhausted
	}
	id, err := store.newID()
	if err != nil || id == "" {
		return Session{}, fmt.Errorf("生成会话标识: %w", ErrInvalidSession)
	}
	if _, exists := store.sessions[id]; exists {
		return Session{}, fmt.Errorf("%w: 会话标识冲突", ErrInvalidSession)
	}
	token, err := store.newToken()
	if err != nil || len(token) < 32 || len(token) > 256 {
		return Session{}, fmt.Errorf("生成会话 token: %w", ErrInvalidSession)
	}
	grants := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" || len(capability) > 128 {
			return Session{}, ErrInvalidSession
		}
		grants[capability] = struct{}{}
	}
	session := Session{
		ID: id, Token: token, Component: component, ComponentVersion: componentVersion,
		IssuedAt: now, LastSeenAt: now, ExpiresAt: now.Add(store.ttl), capabilities: grants,
	}
	store.sessions[id] = session
	event := store.nextEventLocked(EventIssued, session, "")
	store.publish(event)
	return cloneSession(session), nil
}

// RenewCredential 轮换 token 并续签到绝对会话期限，旧 token 立即失效。
func (store *SessionStore) RenewCredential(id, token string) (Session, error) {
	if store == nil || id == "" || token == "" {
		return Session{}, ErrInvalidSession
	}
	now := store.now().UTC()
	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists {
		store.mu.Unlock()
		return Session{}, ErrInvalidSession
	}
	if !session.ExpiresAt.After(now) || !session.IssuedAt.Add(store.maxLifetime).After(now) {
		delete(store.sessions, id)
		event := store.nextEventLocked(EventExpired, session, "ttl_expired")
		store.publish(event)
		store.mu.Unlock()
		return Session{}, ErrSessionExpired
	}
	if len(token) != len(session.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(session.Token)) != 1 {
		store.mu.Unlock()
		return Session{}, ErrInvalidSession
	}
	if store.lockIfIdleLocked(&session, now) {
		store.mu.Unlock()
		return Session{}, ErrSessionLocked
	}
	nextToken, err := store.newToken()
	if err != nil || len(nextToken) < 32 || len(nextToken) > 256 || subtle.ConstantTimeCompare([]byte(nextToken), []byte(session.Token)) == 1 {
		store.mu.Unlock()
		return Session{}, fmt.Errorf("生成续签 token: %w", ErrInvalidSession)
	}
	session.Token = nextToken
	session.LastSeenAt = now
	session.ExpiresAt = now.Add(store.ttl)
	absoluteExpiry := session.IssuedAt.Add(store.maxLifetime)
	if session.ExpiresAt.After(absoluteExpiry) {
		session.ExpiresAt = absoluteExpiry
	}
	store.sessions[id] = session
	event := store.nextEventLocked(EventRenewed, session, "token_rotated")
	store.publish(event)
	store.mu.Unlock()
	return cloneSession(session), nil
}

// ValidateCredential 同时校验会话标识与不可预测 token，避免仅凭可见标识复用会话。
func (store *SessionStore) ValidateCredential(id, token string) (Session, error) {
	if store == nil || id == "" || token == "" {
		return Session{}, ErrInvalidSession
	}
	now := store.now().UTC()
	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists {
		store.mu.Unlock()
		return Session{}, ErrInvalidSession
	}
	if !session.ExpiresAt.After(now) || !session.IssuedAt.Add(store.maxLifetime).After(now) {
		delete(store.sessions, id)
		event := store.nextEventLocked(EventExpired, session, "ttl_expired")
		store.publish(event)
		store.mu.Unlock()
		return Session{}, ErrSessionExpired
	}
	if len(token) != len(session.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(session.Token)) != 1 {
		store.mu.Unlock()
		return Session{}, ErrInvalidSession
	}
	if store.lockIfIdleLocked(&session, now) {
		store.mu.Unlock()
		return Session{}, ErrSessionLocked
	}
	session.LastSeenAt = now
	store.sessions[id] = session
	store.mu.Unlock()
	return cloneSession(session), nil
}

func (store *SessionStore) Validate(id string) (Session, error) {
	if id == "" {
		return Session{}, ErrInvalidSession
	}
	now := store.now().UTC()
	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists {
		store.mu.Unlock()
		return Session{}, ErrInvalidSession
	}
	if !session.ExpiresAt.After(now) || !session.IssuedAt.Add(store.maxLifetime).After(now) {
		delete(store.sessions, id)
		event := store.nextEventLocked(EventExpired, session, "ttl_expired")
		store.publish(event)
		store.mu.Unlock()
		return Session{}, ErrSessionExpired
	}
	if store.lockIfIdleLocked(&session, now) {
		store.mu.Unlock()
		return Session{}, ErrSessionLocked
	}
	cloned := cloneSession(session)
	cloned.Token = ""
	store.mu.Unlock()
	return cloned, nil
}

func (store *SessionStore) Revoke(id string) {
	store.RevokeWithReason(id, RevokeReasonUnspecified)
}

func (store *SessionStore) RevokeWithReason(id, reason string) bool {
	if store == nil || id == "" || !validRevokeReason(reason) {
		return false
	}
	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists {
		store.mu.Unlock()
		return false
	}
	delete(store.sessions, id)
	event := store.nextEventLocked(EventRevoked, session, reason)
	store.publish(event)
	store.mu.Unlock()
	return true
}

func (store *SessionStore) RevokeCredential(id, token, reason string) bool {
	if store == nil || id == "" || token == "" || !validRevokeReason(reason) {
		return false
	}
	store.mu.Lock()
	session, exists := store.sessions[id]
	if !exists || len(token) != len(session.Token) || subtle.ConstantTimeCompare([]byte(token), []byte(session.Token)) != 1 {
		store.mu.Unlock()
		return false
	}
	delete(store.sessions, id)
	event := store.nextEventLocked(EventRevoked, session, reason)
	store.publish(event)
	store.mu.Unlock()
	return true
}

func (store *SessionStore) removeExpired(now time.Time) {
	for id, session := range store.sessions {
		if !session.ExpiresAt.After(now) || !session.IssuedAt.Add(store.maxLifetime).After(now) {
			delete(store.sessions, id)
			store.publish(store.nextEventLocked(EventExpired, session, "ttl_expired"))
		}
	}
}

func (store *SessionStore) nextEventLocked(kind EventKind, session Session, reason string) Event {
	store.sequence++
	return Event{Sequence: store.sequence, Kind: kind, SessionID: session.ID, Component: session.Component,
		ComponentVersion: session.ComponentVersion, OccurredAt: store.now().UTC(), ExpiresAt: session.ExpiresAt, Reason: reason}
}

func (store *SessionStore) publish(event Event) {
	// 会话存储是授权事实源；订阅者若发生背压，应通过 sequence gap 重新握手。
	_ = store.events.Publish(context.Background(), event)
}

func validRevokeReason(reason string) bool {
	switch reason {
	case RevokeReasonUnspecified, RevokeReasonClientShutdown, RevokeReasonReplaced, RevokeReasonPolicyChanged:
		return true
	default:
		return false
	}
}

func cloneSession(session Session) Session {
	copy := session
	copy.capabilities = make(map[string]struct{}, len(session.capabilities))
	for capability := range session.capabilities {
		copy.capabilities[capability] = struct{}{}
	}
	return copy
}

func randomSessionID() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
