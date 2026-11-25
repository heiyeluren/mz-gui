package windowstate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (store *SQLiteStore) SavePage(ctx context.Context, snapshot Snapshot) error {
	if store == nil || store.database == nil || ctx == nil {
		return ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cloned, err := validatePageSnapshot(snapshot, store.maxPayloadBytes)
	if err != nil {
		return err
	}
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始保存页面状态: %w", err)
	}
	defer tx.Rollback()
	var existing uint64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM mzfw_window_page_state WHERE window_id = ? AND route_id = ?`, cloned.WindowID, cloned.RouteID).Scan(&existing)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx, `INSERT INTO mzfw_window_page_state(window_id, route_id, revision, payload, saved_at) VALUES (?, ?, ?, ?, ?)`,
			cloned.WindowID, cloned.RouteID, cloned.Revision, cloned.Payload, cloned.SavedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("插入页面状态: %w", err)
		}
	case err != nil:
		return fmt.Errorf("读取页面 revision: %w", err)
	case snapshot.Revision < existing:
		return ErrStaleRevision
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE mzfw_window_page_state SET revision = ?, payload = ?, saved_at = ? WHERE window_id = ? AND route_id = ?`,
			cloned.Revision, cloned.Payload, cloned.SavedAt.UTC().Format(time.RFC3339Nano), cloned.WindowID, cloned.RouteID); err != nil {
			return fmt.Errorf("更新页面状态: %w", err)
		}
	}
	return tx.Commit()
}

func (store *SQLiteStore) LoadPage(ctx context.Context, windowID, routeID string) (Snapshot, error) {
	if store == nil || store.database == nil || ctx == nil {
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
	var snapshot Snapshot
	var savedAt string
	err := store.database.QueryRowContext(ctx, `SELECT window_id, route_id, revision, payload, saved_at FROM mzfw_window_page_state WHERE window_id = ? AND route_id = ?`, windowID, routeID).
		Scan(&snapshot.WindowID, &snapshot.RouteID, &snapshot.Revision, &snapshot.Payload, &savedAt)
	if err == sql.ErrNoRows {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取页面状态: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, savedAt)
	if err != nil {
		return Snapshot{}, fmt.Errorf("解析页面状态时间: %w", err)
	}
	snapshot.SavedAt = parsed
	return snapshot, nil
}

func (store *SQLiteStore) ListPages(ctx context.Context, windowID string) ([]Snapshot, error) {
	if store == nil || store.database == nil || ctx == nil {
		return nil, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return nil, ErrInvalidSnapshot
	}
	rows, err := store.database.QueryContext(ctx, `SELECT window_id, route_id, revision, payload, saved_at FROM mzfw_window_page_state WHERE window_id = ? ORDER BY route_id`, windowID)
	if err != nil {
		return nil, fmt.Errorf("列出页面状态: %w", err)
	}
	defer rows.Close()
	pages := make([]Snapshot, 0)
	for rows.Next() {
		var snapshot Snapshot
		var savedAt string
		if err := rows.Scan(&snapshot.WindowID, &snapshot.RouteID, &snapshot.Revision, &snapshot.Payload, &savedAt); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, savedAt)
		if err != nil {
			return nil, err
		}
		snapshot.SavedAt = parsed
		pages = append(pages, snapshot)
	}
	return pages, rows.Err()
}

func (store *SQLiteStore) RestorePages(ctx context.Context, fromWindowID, toWindowID string) error {
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

var _ PageStore = (*MemoryStore)(nil)
var _ PageStore = (*SQLiteStore)(nil)
