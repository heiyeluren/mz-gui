package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidQuery = errors.New("审计查询参数无效")

type Query struct {
	Actor        string
	Action       string
	Capability   string
	ResourceType string
	ResourceID   string
	Decision     string
	Limit        int
	Cursor       string
}

type Page struct {
	Records    []Record
	NextCursor string
}

type SQLiteStore struct {
	database *sql.DB
}

type auditCursor struct {
	OccurredAt string `json:"occurred_at"`
	AuditID    string `json:"audit_id"`
}

func NewSQLiteStore(database *sql.DB) *SQLiteStore {
	return &SQLiteStore{database: database}
}

func (store *SQLiteStore) Append(ctx context.Context, event Event) (Record, error) {
	if store == nil || store.database == nil || ctx == nil {
		return Record{}, ErrInvalidQuery
	}
	record, err := event.Record()
	if err != nil {
		return Record{}, err
	}
	_, err = store.database.ExecContext(ctx, `INSERT INTO mzfw_audit_events (
audit_id, occurred_at, actor, action, capability, resource_type, resource_id, decision, reason_code, request_id, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.AuditID, record.OccurredAt, record.Actor, record.Action, nullable(record.Capability), nullable(record.ResourceType),
		nullable(record.ResourceID), record.Decision, nullable(record.ReasonCode), nullable(record.RequestID), record.MetadataJSON)
	if err != nil {
		return Record{}, fmt.Errorf("写入审计事件: %w", err)
	}
	return record, nil
}

func (store *SQLiteStore) AppendEvent(ctx context.Context, event Event) (Record, error) {
	return store.Append(ctx, event)
}

func (store *SQLiteStore) List(ctx context.Context, query Query) (Page, error) {
	if store == nil || store.database == nil || ctx == nil {
		return Page{}, ErrInvalidQuery
	}
	limit, err := normalizeLimit(query.Limit)
	if err != nil {
		return Page{}, err
	}
	where := []string{"1=1"}
	args := []any{}
	if err := appendIdentifierFilter(&where, &args, "actor", query.Actor); err != nil {
		return Page{}, err
	}
	if err := appendIdentifierFilter(&where, &args, "action", query.Action); err != nil {
		return Page{}, err
	}
	if err := appendIdentifierFilter(&where, &args, "capability", query.Capability); err != nil {
		return Page{}, err
	}
	if err := appendIdentifierFilter(&where, &args, "resource_type", query.ResourceType); err != nil {
		return Page{}, err
	}
	if err := appendIdentifierFilter(&where, &args, "resource_id", query.ResourceID); err != nil {
		return Page{}, err
	}
	if strings.TrimSpace(query.Decision) != "" {
		switch Decision(query.Decision) {
		case DecisionAllow, DecisionDeny, DecisionError:
			where = append(where, "decision = ?")
			args = append(args, query.Decision)
		default:
			return Page{}, ErrInvalidQuery
		}
	}
	cursor, err := decodeCursor(query.Cursor)
	if err != nil {
		return Page{}, err
	}
	if cursor != nil {
		where = append(where, "(occurred_at < ? OR (occurred_at = ? AND audit_id < ?))")
		args = append(args, cursor.OccurredAt, cursor.OccurredAt, cursor.AuditID)
	}
	args = append(args, limit+1)
	rows, err := store.database.QueryContext(ctx, `SELECT audit_id, occurred_at, actor, action, capability, resource_type,
resource_id, decision, reason_code, request_id, metadata_json FROM mzfw_audit_events WHERE `+strings.Join(where, " AND ")+
		` ORDER BY occurred_at DESC, audit_id DESC LIMIT ?`, args...)
	if err != nil {
		return Page{}, fmt.Errorf("查询审计事件: %w", err)
	}
	defer rows.Close()
	records := make([]Record, 0, limit+1)
	for rows.Next() {
		record, scanErr := scanRecord(rows)
		if scanErr != nil {
			return Page{}, scanErr
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("读取审计事件: %w", err)
	}
	page := Page{Records: records}
	if len(page.Records) > limit {
		last := page.Records[limit-1]
		page.NextCursor = encodeCursor(auditCursor{OccurredAt: last.OccurredAt, AuditID: last.AuditID})
		page.Records = page.Records[:limit]
	}
	return page, nil
}

func normalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return 100, nil
	}
	if limit < 1 || limit > 500 {
		return 0, ErrInvalidQuery
	}
	return limit, nil
}

func appendIdentifierFilter(where *[]string, args *[]any, column, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !identifierPattern.MatchString(value) {
		return ErrInvalidQuery
	}
	*where = append(*where, column+" = ?")
	*args = append(*args, value)
	return nil
}

func encodeCursor(cursor auditCursor) string {
	data, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeCursor(value string) (*auditCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidQuery
	}
	var cursor auditCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, ErrInvalidQuery
	}
	if _, err := time.Parse(time.RFC3339Nano, cursor.OccurredAt); err != nil || !identifierPattern.MatchString(cursor.AuditID) {
		return nil, ErrInvalidQuery
	}
	return &cursor, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func scanRecord(rows *sql.Rows) (Record, error) {
	var record Record
	var capability, resourceType, resourceID, reasonCode, requestID sql.NullString
	if err := rows.Scan(&record.AuditID, &record.OccurredAt, &record.Actor, &record.Action, &capability, &resourceType,
		&resourceID, &record.Decision, &reasonCode, &requestID, &record.MetadataJSON); err != nil {
		return Record{}, fmt.Errorf("读取审计事件字段: %w", err)
	}
	record.Capability = capability.String
	record.ResourceType = resourceType.String
	record.ResourceID = resourceID.String
	record.ReasonCode = reasonCode.String
	record.RequestID = requestID.String
	return record, nil
}
