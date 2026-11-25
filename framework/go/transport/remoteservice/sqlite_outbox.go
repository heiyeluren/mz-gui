package remoteservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	frameworkclock "github.com/heiyeluren/mz-gui/framework/go/core/clock"
)

type SQLiteOutboxOptions struct {
	Clock frameworkclock.Clock
}

type SQLiteOutbox struct {
	database *sql.DB
	clock    frameworkclock.Clock
}

func NewSQLiteOutbox(database *sql.DB, options SQLiteOutboxOptions) (*SQLiteOutbox, error) {
	if database == nil {
		return nil, ErrInvalidConfig
	}
	clock := options.Clock
	if clock == nil {
		clock = frameworkclock.System{}
	}
	store := &SQLiteOutbox{database: database, clock: clock}
	if err := store.migrate(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *SQLiteOutbox) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

func (store *SQLiteOutbox) Enqueue(ctx context.Context, entry OutboxEntry) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil {
		return OutboxEntry{}, ErrInvalidRequest
	}
	if err := validateOutboxEntry(entry); err != nil {
		return OutboxEntry{}, err
	}
	now := store.clock.Now().UTC()
	if entry.Status == "" {
		entry.Status = OutboxStatusPending
	}
	if entry.NextAt.IsZero() {
		entry.NextAt = now
	}
	entry.CreatedAt = now
	entry.UpdatedAt = now
	headersJSON, err := marshalHeader(entry.Request.Headers)
	if err != nil {
		return OutboxEntry{}, err
	}
	dependsJSON, err := marshalStringSlice(entry.DependsOn)
	if err != nil {
		return OutboxEntry{}, err
	}
	_, err = store.database.ExecContext(ctx, `INSERT INTO mzfw_remote_outbox (
id, operation, method, url, headers_json, request_id, idempotency_key,
payload_uri, payload_sha256, payload_size_bytes, payload_media_type,
depends_on_json, attempts, next_at, status, last_error, conflict_policy, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Operation, entry.Request.Method, entry.Request.URL, headersJSON,
		entry.Request.RequestID, entry.Request.IdempotencyKey, entry.Request.Payload.URI,
		entry.Request.Payload.SHA256, entry.Request.Payload.SizeBytes, entry.Request.Payload.MediaType,
		dependsJSON, entry.Attempts, formatOutboxTime(entry.NextAt), entry.Status,
		entry.LastError, entry.ConflictPolicy, formatOutboxTime(entry.CreatedAt), formatOutboxTime(entry.UpdatedAt))
	if err != nil {
		return OutboxEntry{}, fmt.Errorf("插入 RemoteService outbox: %w", err)
	}
	return cloneOutboxEntry(entry), nil
}

func (store *SQLiteOutbox) Get(ctx context.Context, id string) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(id) == "" {
		return OutboxEntry{}, ErrInvalidRequest
	}
	row := store.database.QueryRowContext(ctx, selectOutboxColumns+" WHERE id = ?", id)
	entry, err := scanOutboxEntry(row)
	if err != nil {
		return OutboxEntry{}, err
	}
	return entry, nil
}

func (store *SQLiteOutbox) ListReady(ctx context.Context, now time.Time, limit int) ([]OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || limit < 1 {
		return nil, ErrInvalidRequest
	}
	rows, err := store.database.QueryContext(ctx, selectOutboxColumns+` WHERE status IN (?, ?) AND next_at <= ?
ORDER BY next_at ASC, created_at ASC, id ASC LIMIT ?`,
		OutboxStatusPending, OutboxStatusRetrying, formatOutboxTime(now), limit*4)
	if err != nil {
		return nil, fmt.Errorf("查询 RemoteService outbox ready: %w", err)
	}
	defer rows.Close()
	ready := make([]OutboxEntry, 0, limit)
	for rows.Next() {
		entry, err := scanOutboxEntry(rows)
		if err != nil {
			return nil, err
		}
		ok, err := store.dependenciesSatisfied(ctx, entry)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		ready = append(ready, entry)
		if len(ready) == limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 RemoteService outbox ready: %w", err)
	}
	return ready, nil
}

func (store *SQLiteOutbox) MarkAttempt(ctx context.Context, id string, nextAt time.Time, lastErr error) (OutboxEntry, error) {
	return store.updateState(ctx, id, OutboxStatusRetrying, nextAt, errorText(lastErr), true)
}

func (store *SQLiteOutbox) MarkSucceeded(ctx context.Context, id string) (OutboxEntry, error) {
	return store.updateState(ctx, id, OutboxStatusSucceeded, time.Time{}, "", true)
}

func (store *SQLiteOutbox) MarkTerminal(ctx context.Context, id string, err error) (OutboxEntry, error) {
	return store.updateState(ctx, id, OutboxStatusTerminal, time.Time{}, errorText(err), true)
}

func (store *SQLiteOutbox) updateState(ctx context.Context, id string, status OutboxStatus, nextAt time.Time, lastError string, increment bool) (OutboxEntry, error) {
	if store == nil || ctx == nil || ctx.Err() != nil || strings.TrimSpace(id) == "" {
		return OutboxEntry{}, ErrInvalidRequest
	}
	current, err := store.Get(ctx, id)
	if err != nil {
		return OutboxEntry{}, err
	}
	if nextAt.IsZero() {
		nextAt = current.NextAt
	}
	attempts := current.Attempts
	if increment {
		attempts++
	}
	updatedAt := store.clock.Now().UTC()
	result, err := store.database.ExecContext(ctx, `UPDATE mzfw_remote_outbox
SET attempts = ?, next_at = ?, status = ?, last_error = ?, updated_at = ?
WHERE id = ?`,
		attempts, formatOutboxTime(nextAt), status, lastError, formatOutboxTime(updatedAt), id)
	if err != nil {
		return OutboxEntry{}, fmt.Errorf("更新 RemoteService outbox: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return OutboxEntry{}, err
	}
	if affected == 0 {
		return OutboxEntry{}, ErrOutboxNotFound
	}
	return store.Get(ctx, id)
}

func (store *SQLiteOutbox) dependenciesSatisfied(ctx context.Context, entry OutboxEntry) (bool, error) {
	for _, dependencyID := range entry.DependsOn {
		var status OutboxStatus
		err := store.database.QueryRowContext(ctx, "SELECT status FROM mzfw_remote_outbox WHERE id = ?", dependencyID).Scan(&status)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("读取 RemoteService outbox dependency: %w", err)
		}
		if status != OutboxStatusSucceeded {
			return false, nil
		}
	}
	return true, nil
}

func (store *SQLiteOutbox) migrate(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mzfw_remote_outbox (
id TEXT PRIMARY KEY,
operation TEXT NOT NULL,
method TEXT NOT NULL,
url TEXT NOT NULL,
headers_json TEXT NOT NULL,
request_id TEXT NOT NULL,
idempotency_key TEXT NOT NULL,
payload_uri TEXT NOT NULL,
payload_sha256 TEXT NOT NULL,
payload_size_bytes INTEGER NOT NULL,
payload_media_type TEXT NOT NULL,
depends_on_json TEXT NOT NULL,
attempts INTEGER NOT NULL,
next_at TEXT NOT NULL,
status TEXT NOT NULL,
last_error TEXT NOT NULL,
conflict_policy TEXT NOT NULL,
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_mzfw_remote_outbox_ready
ON mzfw_remote_outbox(status, next_at, created_at, id);`)
	if err != nil {
		return fmt.Errorf("迁移 RemoteService outbox: %w", err)
	}
	return nil
}

const selectOutboxColumns = `SELECT id, operation, method, url, headers_json, request_id, idempotency_key,
payload_uri, payload_sha256, payload_size_bytes, payload_media_type,
depends_on_json, attempts, next_at, status, last_error, conflict_policy, created_at, updated_at
FROM mzfw_remote_outbox`

type outboxScanner interface {
	Scan(...any) error
}

func scanOutboxEntry(scanner outboxScanner) (OutboxEntry, error) {
	var entry OutboxEntry
	var headersJSON string
	var dependsJSON string
	var nextAt string
	var createdAt string
	var updatedAt string
	err := scanner.Scan(&entry.ID, &entry.Operation, &entry.Request.Method, &entry.Request.URL,
		&headersJSON, &entry.Request.RequestID, &entry.Request.IdempotencyKey,
		&entry.Request.Payload.URI, &entry.Request.Payload.SHA256, &entry.Request.Payload.SizeBytes,
		&entry.Request.Payload.MediaType, &dependsJSON, &entry.Attempts, &nextAt, &entry.Status,
		&entry.LastError, &entry.ConflictPolicy, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return OutboxEntry{}, ErrOutboxNotFound
	}
	if err != nil {
		return OutboxEntry{}, fmt.Errorf("扫描 RemoteService outbox: %w", err)
	}
	headers, err := unmarshalHeader(headersJSON)
	if err != nil {
		return OutboxEntry{}, err
	}
	dependsOn, err := unmarshalStringSlice(dependsJSON)
	if err != nil {
		return OutboxEntry{}, err
	}
	entry.Request.Headers = headers
	entry.DependsOn = dependsOn
	if entry.NextAt, err = parseOutboxTime(nextAt); err != nil {
		return OutboxEntry{}, err
	}
	if entry.CreatedAt, err = parseOutboxTime(createdAt); err != nil {
		return OutboxEntry{}, err
	}
	if entry.UpdatedAt, err = parseOutboxTime(updatedAt); err != nil {
		return OutboxEntry{}, err
	}
	return cloneOutboxEntry(entry), nil
}

func marshalHeader(headers http.Header) (string, error) {
	data, err := json.Marshal(cloneHeader(headers))
	if err != nil {
		return "", ErrInvalidRequest
	}
	return string(data), nil
}

func unmarshalHeader(data string) (http.Header, error) {
	var headers http.Header
	if err := json.Unmarshal([]byte(data), &headers); err != nil {
		return nil, ErrInvalidRequest
	}
	return cloneHeader(headers), nil
}

func marshalStringSlice(values []string) (string, error) {
	data, err := json.Marshal(append([]string(nil), values...))
	if err != nil {
		return "", ErrInvalidRequest
	}
	return string(data), nil
}

func unmarshalStringSlice(data string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(data), &values); err != nil {
		return nil, ErrInvalidRequest
	}
	return append([]string(nil), values...), nil
}

func formatOutboxTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOutboxTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidRequest
	}
	return parsed.UTC(), nil
}
