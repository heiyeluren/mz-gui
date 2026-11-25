package wailsapp

import (
	"context"
	"strings"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
)

func PersistPageState(ctx context.Context, store windowstate.PageStore, windowID, routeID, payload string) error {
	if store == nil || ctx == nil {
		return windowstate.ErrInvalidSnapshot
	}
	revision := uint64(1)
	if existing, err := store.LoadPage(ctx, windowID, routeID); err == nil {
		revision = existing.Revision + 1
	}
	return store.SavePage(ctx, windowstate.Snapshot{
		WindowID: windowID, RouteID: routeID, Revision: revision, Payload: []byte(payload),
	})
}

func RestoreWindowPages(ctx context.Context, store windowstate.PageStore, fromWindowID, toWindowID string) error {
	if store == nil || ctx == nil || strings.TrimSpace(fromWindowID) == "" || strings.TrimSpace(toWindowID) == "" {
		return windowstate.ErrInvalidSnapshot
	}
	return store.RestorePages(ctx, fromWindowID, toWindowID)
}
