package authorization

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	defaultOneTimeGrantTTL      = 30 * time.Second
	defaultOneTimeGrantCapacity = 128
	maxOneTimeGrantCapacity     = 4096
	oneTimeGrantTokenBytes      = 32
)

var (
	ErrInvalidOneTimeGrant  = errors.New("一次性授权无效或已过期")
	ErrOneTimeGrantCapacity = errors.New("一次性授权数量超过上限")
)

type OneTimeGrantOptions struct {
	TTL      time.Duration
	Capacity int
	Now      func() time.Time
	Random   io.Reader
}

type oneTimeGrant struct {
	slot      string
	subject   string
	action    string
	resource  string
	expiresAt time.Time
}

// OneTimeGrantStore 只在内存中保存 token 摘要，并在首次消费尝试时立即删除。
type OneTimeGrantStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	capacity int
	now      func() time.Time
	random   io.Reader
	entries  map[[sha256.Size]byte]oneTimeGrant
	slots    map[string][sha256.Size]byte
}

func NewOneTimeGrantStore(options OneTimeGrantOptions) (*OneTimeGrantStore, error) {
	if options.TTL == 0 {
		options.TTL = defaultOneTimeGrantTTL
	}
	if options.Capacity == 0 {
		options.Capacity = defaultOneTimeGrantCapacity
	}
	if options.TTL < time.Second || options.TTL > 5*time.Minute || options.Capacity < 1 || options.Capacity > maxOneTimeGrantCapacity {
		return nil, errors.New("一次性授权配置无效")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	return &OneTimeGrantStore{
		ttl: options.TTL, capacity: options.Capacity, now: options.Now, random: options.Random,
		entries: make(map[[sha256.Size]byte]oneTimeGrant, options.Capacity),
		slots:   make(map[string][sha256.Size]byte, options.Capacity),
	}, nil
}

func (store *OneTimeGrantStore) Issue(subject, action, resource string) (string, time.Time, error) {
	return store.issue("", subject, action, resource)
}

// IssueReplacing 保证同一 slot 最多只有一个有效 token，适用于可重试的授权签发 RPC。
func (store *OneTimeGrantStore) IssueReplacing(slot, subject, action, resource string) (string, time.Time, error) {
	if !validGrantBinding(slot) {
		return "", time.Time{}, ErrInvalidOneTimeGrant
	}
	return store.issue(slot, subject, action, resource)
}

func (store *OneTimeGrantStore) issue(slot, subject, action, resource string) (string, time.Time, error) {
	if store == nil || !validGrantBinding(subject) || !validGrantBinding(action) || !validGrantBinding(resource) {
		return "", time.Time{}, ErrInvalidOneTimeGrant
	}
	now := store.now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.removeExpiredLocked(now)
	if previous, exists := store.slots[slot]; slot != "" && exists {
		delete(store.entries, previous)
		delete(store.slots, slot)
	}
	if len(store.entries) >= store.capacity {
		return "", time.Time{}, ErrOneTimeGrantCapacity
	}
	for attempts := 0; attempts < 4; attempts++ {
		raw := make([]byte, oneTimeGrantTokenBytes)
		if _, err := io.ReadFull(store.random, raw); err != nil {
			return "", time.Time{}, errors.New("生成一次性授权失败")
		}
		token := hex.EncodeToString(raw)
		fingerprint := sha256.Sum256([]byte(token))
		if _, exists := store.entries[fingerprint]; exists {
			continue
		}
		expiresAt := now.Add(store.ttl)
		store.entries[fingerprint] = oneTimeGrant{slot: slot, subject: subject, action: action, resource: resource, expiresAt: expiresAt}
		if slot != "" {
			store.slots[slot] = fingerprint
		}
		return token, expiresAt, nil
	}
	return "", time.Time{}, errors.New("生成一次性授权失败")
}

func (store *OneTimeGrantStore) Consume(token, subject, action, resource string) error {
	if store == nil || len(token) != oneTimeGrantTokenBytes*2 || !validGrantBinding(subject) || !validGrantBinding(action) || !validGrantBinding(resource) {
		return ErrInvalidOneTimeGrant
	}
	fingerprint := sha256.Sum256([]byte(token))
	now := store.now().UTC()
	store.mu.Lock()
	grant, exists := store.entries[fingerprint]
	delete(store.entries, fingerprint)
	if exists && grant.slot != "" && store.slots[grant.slot] == fingerprint {
		delete(store.slots, grant.slot)
	}
	store.mu.Unlock()
	if !exists || !now.Before(grant.expiresAt) || grant.subject != subject || grant.action != action || grant.resource != resource {
		return ErrInvalidOneTimeGrant
	}
	return nil
}

func (store *OneTimeGrantStore) RevokeSlot(slot string) (bool, error) {
	if store == nil || !validGrantBinding(slot) {
		return false, ErrInvalidOneTimeGrant
	}
	now := store.now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()
	store.removeExpiredLocked(now)
	fingerprint, exists := store.slots[slot]
	if !exists {
		return false, nil
	}
	delete(store.entries, fingerprint)
	delete(store.slots, slot)
	return true, nil
}

func (store *OneTimeGrantStore) removeExpiredLocked(now time.Time) {
	for fingerprint, grant := range store.entries {
		if !now.Before(grant.expiresAt) {
			delete(store.entries, fingerprint)
			if grant.slot != "" && store.slots[grant.slot] == fingerprint {
				delete(store.slots, grant.slot)
			}
		}
	}
}

func validGrantBinding(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 256
}
