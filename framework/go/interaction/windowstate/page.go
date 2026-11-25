package windowstate

import (
	"context"
	"sort"
	"strings"
	"time"
)

type PageStore interface {
	SavePage(context.Context, Snapshot) error
	LoadPage(context.Context, string, string) (Snapshot, error)
	ListPages(context.Context, string) ([]Snapshot, error)
	RestorePages(context.Context, string, string) error
}

func pageKey(windowID, routeID string) string {
	return strings.TrimSpace(windowID) + "\x1f" + strings.TrimSpace(routeID)
}

func validatePageSnapshot(snapshot Snapshot, maxPayload int64) (Snapshot, error) {
	windowID := strings.TrimSpace(snapshot.WindowID)
	routeID := strings.TrimSpace(snapshot.RouteID)
	if windowID == "" || routeID == "" || snapshot.Revision == 0 {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if int64(len(snapshot.Payload)) > maxPayload {
		return Snapshot{}, ErrPayloadTooLarge
	}
	cloned := snapshot
	cloned.WindowID = windowID
	cloned.RouteID = routeID
	cloned.Payload = append([]byte(nil), snapshot.Payload...)
	if cloned.SavedAt.IsZero() {
		cloned.SavedAt = time.Now().UTC()
	}
	return cloned, nil
}

func (store *MemoryStore) SavePage(ctx context.Context, snapshot Snapshot) error {
	if store == nil || ctx == nil {
		return ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cloned, err := validatePageSnapshot(snapshot, store.maxPayloadBytes)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.pages == nil {
		store.pages = make(map[string]Snapshot)
	}
	key := pageKey(cloned.WindowID, cloned.RouteID)
	if existing, ok := store.pages[key]; ok && snapshot.Revision < existing.Revision {
		return ErrStaleRevision
	}
	store.pages[key] = cloned
	return nil
}

func (store *MemoryStore) LoadPage(ctx context.Context, windowID, routeID string) (Snapshot, error) {
	if store == nil || ctx == nil {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	windowID = strings.TrimSpace(windowID)
	routeID = strings.TrimSpace(routeID)
	if windowID == "" || routeID == "" {
		return Snapshot{}, ErrInvalidSnapshot
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, ok := store.pages[pageKey(windowID, routeID)]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	return snapshot, nil
}

func (store *MemoryStore) ListPages(ctx context.Context, windowID string) ([]Snapshot, error) {
	if store == nil || ctx == nil {
		return nil, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return nil, ErrInvalidSnapshot
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	pages := make([]Snapshot, 0)
	prefix := windowID + "\x1f"
	for key, snapshot := range store.pages {
		if strings.HasPrefix(key, prefix) {
			cloned := snapshot
			cloned.Payload = append([]byte(nil), snapshot.Payload...)
			pages = append(pages, cloned)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].RouteID < pages[j].RouteID })
	return pages, nil
}

func (store *MemoryStore) RestorePages(ctx context.Context, fromWindowID, toWindowID string) error {
	pages, err := store.ListPages(ctx, fromWindowID)
	if err != nil {
		return err
	}
	toWindowID = strings.TrimSpace(toWindowID)
	if toWindowID == "" {
		return ErrInvalidSnapshot
	}
	for _, snapshot := range pages {
		snapshot.WindowID = toWindowID
		if err := store.SavePage(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}
