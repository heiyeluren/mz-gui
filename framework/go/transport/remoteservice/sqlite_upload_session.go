package remoteservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type SQLiteUploadSessionStoreOptions struct {
	Clock frameworkclock.Clock
}

type SQLiteUploadSessionStore struct {
	database *sql.DB
	clock    frameworkclock.Clock
}

func NewSQLiteUploadSessionStore(database *sql.DB, options SQLiteUploadSessionStoreOptions) (*SQLiteUploadSessionStore, error) {
	if database == nil {
		return nil, ErrInvalidConfig
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	store := &SQLiteUploadSessionStore{database: database, clock: clock}
	if err := store.migrate(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *SQLiteUploadSessionStore) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

func (store *SQLiteUploadSessionStore) LoadUploadSession(ctx context.Context, sessionID string) (UploadSessionState, bool, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(sessionID) == "" {
		return UploadSessionState{}, false, ErrInvalidUpload
	}
	row := store.database.QueryRowContext(ctx, `SELECT session_id, total_bytes, chunk_size, total_chunks, completed_chunks_json, updated_at
FROM mzfw_remote_upload_sessions WHERE session_id = ?`, sessionID)
	var state UploadSessionState
	var completedJSON string
	var updatedAt string
	err := row.Scan(&state.SessionID, &state.TotalBytes, &state.ChunkSize, &state.TotalChunks, &completedJSON, &updatedAt)
	if err == sql.ErrNoRows {
		return UploadSessionState{}, false, nil
	}
	if err != nil {
		return UploadSessionState{}, false, fmt.Errorf("读取 RemoteService 上传 session: %w", err)
	}
	if err := json.Unmarshal([]byte(completedJSON), &state.CompletedChunks); err != nil {
		return UploadSessionState{}, false, ErrInvalidUpload
	}
	if state.CompletedChunks == nil {
		state.CompletedChunks = map[int]UploadCompletedChunk{}
	}
	if state.UpdatedAt, err = parseOutboxTime(updatedAt); err != nil {
		return UploadSessionState{}, false, err
	}
	return state.Clone(), true, nil
}

func (store *SQLiteUploadSessionStore) SaveUploadSession(ctx context.Context, state UploadSessionState) error {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(state.SessionID) == "" || state.TotalBytes <= 0 || state.ChunkSize <= 0 || state.TotalChunks <= 0 {
		return ErrInvalidUpload
	}
	completedJSON, err := json.Marshal(state.Clone().CompletedChunks)
	if err != nil {
		return ErrInvalidUpload
	}
	updatedAt := state.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = store.clock.Now()
	}
	_, err = store.database.ExecContext(ctx, `INSERT INTO mzfw_remote_upload_sessions (
session_id, total_bytes, chunk_size, total_chunks, completed_chunks_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
total_bytes = excluded.total_bytes,
chunk_size = excluded.chunk_size,
total_chunks = excluded.total_chunks,
completed_chunks_json = excluded.completed_chunks_json,
updated_at = excluded.updated_at`,
		state.SessionID, state.TotalBytes, state.ChunkSize, state.TotalChunks, string(completedJSON), formatOutboxTime(updatedAt))
	if err != nil {
		return fmt.Errorf("保存 RemoteService 上传 session: %w", err)
	}
	return nil
}

func (store *SQLiteUploadSessionStore) migrate(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mzfw_remote_upload_sessions (
session_id TEXT PRIMARY KEY,
total_bytes INTEGER NOT NULL,
chunk_size INTEGER NOT NULL,
total_chunks INTEGER NOT NULL,
completed_chunks_json TEXT NOT NULL,
updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_mzfw_remote_upload_sessions_updated
ON mzfw_remote_upload_sessions(updated_at);`)
	if err != nil {
		return fmt.Errorf("迁移 RemoteService 上传 session: %w", err)
	}
	return nil
}

var _ UploadSessionStore = (*SQLiteUploadSessionStore)(nil)
