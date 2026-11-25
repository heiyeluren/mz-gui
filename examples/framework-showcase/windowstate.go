package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
)

func (service *ShowcaseService) persistWindowState(ctx context.Context) error {
	if service == nil {
		return windowstate.ErrInvalidSnapshot
	}
	dir := service.windowStateDir
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(cache, "mz-gui-framework-showcase")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := persistMemoryWindowState(ctx); err != nil {
		return err
	}
	store, err := windowstate.OpenSQLiteStore(ctx, filepath.Join(dir, "window-state.sqlite"), windowstate.Options{})
	if err != nil {
		return err
	}
	defer store.Close()
	if err := bindShowcaseWindowPages(ctx, store); err != nil {
		return err
	}
	revision := uint64(1)
	if existing, err := store.Load(ctx, "main"); err == nil {
		revision = existing.Revision + 1
	} else if err != nil && !errors.Is(err, windowstate.ErrNotFound) {
		return err
	}
	if err := store.Save(ctx, windowstate.Snapshot{
		WindowID: "preview",
		RouteID:  "showcase",
		Revision: 1,
		Payload:  []byte(`{"preview":true}`),
	}); err != nil {
		return err
	}
	if err := store.Delete(ctx, "preview"); err != nil {
		return err
	}
	return store.Save(ctx, windowstate.Snapshot{
		WindowID: "main",
		RouteID:  "showcase",
		Revision: revision,
		Payload:  []byte(`{"ready":true}`),
	})
}

func bindShowcaseWindowPages(ctx context.Context, store windowstate.PageStore) error {
	if ctx == nil || store == nil {
		return windowstate.ErrInvalidBinder
	}
	binder, err := windowstate.NewBinder(store)
	if err != nil {
		return err
	}
	buffer := &windowstate.Buffer{}
	buffer.Put("main", "showcase", []byte(`{"ready":true}`))
	if err := binder.Register(buffer.Codec("showcase")); err != nil {
		return err
	}
	if err := binder.Require("showcase"); err != nil {
		return err
	}
	if err := binder.SaveAll(ctx, "main"); err != nil {
		return err
	}
	return binder.RestoreAll(ctx, "main")
}

func persistMemoryWindowState(ctx context.Context) error {
	store := windowstate.NewMemoryStore(windowstate.Options{})
	if err := store.Save(ctx, windowstate.Snapshot{
		WindowID: "main",
		RouteID:  "showcase",
		Revision: 1,
		Payload:  []byte(`{"ready":true}`),
	}); err != nil {
		return err
	}
	if _, err := store.Load(ctx, "main"); err != nil {
		return err
	}
	if err := store.Save(ctx, windowstate.Snapshot{
		WindowID: "preview",
		RouteID:  "showcase",
		Revision: 1,
		Payload:  []byte(`{"preview":true}`),
	}); err != nil {
		return err
	}
	return store.Delete(ctx, "preview")
}
