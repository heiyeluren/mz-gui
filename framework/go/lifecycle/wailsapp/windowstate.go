package wailsapp

import (
	"context"
	"fmt"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func PersistWindowState(ctx context.Context, store windowstate.Store, windowID, routeID, payload string) error {
	if store == nil || ctx == nil {
		return windowstate.ErrInvalidSnapshot
	}
	revision := uint64(1)
	if existing, err := store.Load(ctx, windowID); err == nil {
		revision = existing.Revision + 1
	}
	return store.Save(ctx, windowstate.Snapshot{
		WindowID: windowID, RouteID: routeID, Revision: revision, Payload: []byte(payload),
	})
}

func BindWindowState(window *application.WebviewWindow, store windowstate.Store, windowID, routeID string) error {
	if window == nil || store == nil {
		return fmt.Errorf("窗口状态绑定参数无效")
	}
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		_ = PersistWindowState(context.Background(), store, windowID, routeID, "{}")
	})
	return nil
}
