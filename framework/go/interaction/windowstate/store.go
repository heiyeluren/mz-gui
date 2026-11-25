package windowstate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const defaultMaxPayloadBytes = 256 * 1024

var (
	ErrInvalidSnapshot = errors.New("窗口状态快照无效")
	ErrNotFound        = errors.New("窗口状态不存在")
	ErrStaleRevision   = errors.New("窗口状态 revision 已过期")
	ErrPayloadTooLarge = errors.New("窗口状态 payload 超过限制")
)

type Snapshot struct {
	WindowID string
	RouteID  string
	Revision uint64
	Payload  []byte
	SavedAt  time.Time
}

type Options struct {
	MaxPayloadBytes int64
}

type Store interface {
	Save(context.Context, Snapshot) error
	Load(context.Context, string) (Snapshot, error)
	Delete(context.Context, string) error
}

type MemoryStore struct {
	mu              sync.RWMutex
	maxPayloadBytes int64
	snapshots       map[string]Snapshot
	pages           map[string]Snapshot
}

func NewMemoryStore(options Options) *MemoryStore {
	maxPayload := options.MaxPayloadBytes
	if maxPayload <= 0 {
		maxPayload = defaultMaxPayloadBytes
	}
	return &MemoryStore{maxPayloadBytes: maxPayload, snapshots: make(map[string]Snapshot), pages: make(map[string]Snapshot)}
}

func (store *MemoryStore) Save(ctx context.Context, snapshot Snapshot) error {
	if store == nil || ctx == nil {
		return ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	windowID := strings.TrimSpace(snapshot.WindowID)
	if windowID == "" || snapshot.Revision == 0 {
		return ErrInvalidSnapshot
	}
	if int64(len(snapshot.Payload)) > store.maxPayloadBytes {
		return ErrPayloadTooLarge
	}
	cloned := snapshot
	cloned.WindowID = windowID
	cloned.Payload = append([]byte(nil), snapshot.Payload...)
	if cloned.SavedAt.IsZero() {
		cloned.SavedAt = time.Now().UTC()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.snapshots[windowID]; ok && snapshot.Revision < existing.Revision {
		return ErrStaleRevision
	}
	store.snapshots[windowID] = cloned
	return nil
}

func (store *MemoryStore) Load(ctx context.Context, windowID string) (Snapshot, error) {
	if store == nil || ctx == nil {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return Snapshot{}, ErrInvalidSnapshot
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, ok := store.snapshots[windowID]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	return snapshot, nil
}

func (store *MemoryStore) Delete(ctx context.Context, windowID string) error {
	if store == nil || ctx == nil {
		return ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return ErrInvalidSnapshot
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.snapshots[windowID]; !ok {
		return ErrNotFound
	}
	delete(store.snapshots, windowID)
	return nil
}
