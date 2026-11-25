package windowstate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/interaction/windowstate/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

type SQLiteStore struct {
	database        *sql.DB
	maxPayloadBytes int64
}

func OpenSQLiteStore(ctx context.Context, path string, options Options) (*SQLiteStore, error) {
	if ctx == nil || strings.TrimSpace(path) == "" {
		return nil, ErrInvalidSnapshot
	}
	database, err := frameworksqlite.Open(ctx, frameworksqlite.Options{
		Path:    path,
		Bundles: []frameworksqlite.MigrationBundle{migrations.Bundle()},
	})
	if err != nil {
		return nil, err
	}
	maxPayload := options.MaxPayloadBytes
	if maxPayload <= 0 {
		maxPayload = defaultMaxPayloadBytes
	}
	return &SQLiteStore{database: database, maxPayloadBytes: maxPayload}, nil
}

func (store *SQLiteStore) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

func (store *SQLiteStore) Save(ctx context.Context, snapshot Snapshot) error {
	if store == nil || store.database == nil || ctx == nil {
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
	savedAt := snapshot.SavedAt
	if savedAt.IsZero() {
		savedAt = time.Now().UTC()
	}
	tx, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始保存窗口状态: %w", err)
	}
	defer tx.Rollback()
	var existing uint64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM mzfw_window_state WHERE window_id = ?`, windowID).Scan(&existing)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx, `INSERT INTO mzfw_window_state(window_id, route_id, revision, payload, saved_at) VALUES (?, ?, ?, ?, ?)`,
			windowID, strings.TrimSpace(snapshot.RouteID), snapshot.Revision, snapshot.Payload, savedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("插入窗口状态: %w", err)
		}
	case err != nil:
		return fmt.Errorf("读取窗口状态 revision: %w", err)
	case snapshot.Revision < existing:
		return ErrStaleRevision
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE mzfw_window_state SET route_id = ?, revision = ?, payload = ?, saved_at = ? WHERE window_id = ?`,
			strings.TrimSpace(snapshot.RouteID), snapshot.Revision, snapshot.Payload, savedAt.UTC().Format(time.RFC3339Nano), windowID); err != nil {
			return fmt.Errorf("更新窗口状态: %w", err)
		}
	}
	return tx.Commit()
}

func (store *SQLiteStore) Load(ctx context.Context, windowID string) (Snapshot, error) {
	if store == nil || store.database == nil || ctx == nil {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return Snapshot{}, ErrInvalidSnapshot
	}
	var snapshot Snapshot
	var savedAt string
	err := store.database.QueryRowContext(ctx, `SELECT window_id, route_id, revision, payload, saved_at FROM mzfw_window_state WHERE window_id = ?`, windowID).
		Scan(&snapshot.WindowID, &snapshot.RouteID, &snapshot.Revision, &snapshot.Payload, &savedAt)
	if err == sql.ErrNoRows {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("读取窗口状态: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, savedAt)
	if err != nil {
		return Snapshot{}, fmt.Errorf("解析窗口状态时间: %w", err)
	}
	snapshot.SavedAt = parsed
	return snapshot, nil
}

func (store *SQLiteStore) Delete(ctx context.Context, windowID string) error {
	if store == nil || store.database == nil || ctx == nil {
		return ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return ErrInvalidSnapshot
	}
	result, err := store.database.ExecContext(ctx, `DELETE FROM mzfw_window_state WHERE window_id = ?`, windowID)
	if err != nil {
		return fmt.Errorf("删除窗口状态: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

var _ Store = (*SQLiteStore)(nil)
