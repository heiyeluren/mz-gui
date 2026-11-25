package scheduler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type storedMutation struct {
	operation      string
	fingerprint    string
	scheduleID     string
	occurrenceID   sql.NullString
	resultRevision int64
}

// CreateIdempotent 在同一事务内提交 schedule 和幂等记录。
func (repository *SQLiteRepository) CreateIdempotent(ctx context.Context, request CreateRequest, key string) (Schedule, bool, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Schedule{}, false, err
	}
	fingerprint, err := definitionFingerprint("schedule.create", "", 0, normalized)
	if err != nil || !validIdempotencyKey(key) {
		return Schedule{}, false, fmt.Errorf("%w: schedule 创建幂等参数无效", ErrInvalidArgument)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, false, fmt.Errorf("开始幂等创建 schedule: %w", err)
	}
	defer transaction.Rollback()
	if stored, found, readErr := readStoredMutation(ctx, transaction, key); readErr != nil {
		return Schedule{}, false, readErr
	} else if found {
		if err := verifyStoredMutation(stored, "schedule.create", fingerprint, ""); err != nil {
			return Schedule{}, false, err
		}
		value, getErr := getSchedule(ctx, transaction, stored.scheduleID, true)
		return value, false, getErr
	}
	value := scheduleFromRequest(repository.newID("schedule"), normalized, 1, normalized.Now, normalized.Now)
	if value.ID == "" {
		return Schedule{}, false, fmt.Errorf("%w: schedule id 生成失败", ErrInvalidArgument)
	}
	if err := insertSchedule(ctx, transaction, value); err != nil {
		return Schedule{}, false, err
	}
	if err := recordStoredMutation(ctx, transaction, key, "schedule.create", fingerprint, value.ID, "", value.Revision, normalized.Now); err != nil {
		return Schedule{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Schedule{}, false, fmt.Errorf("提交幂等创建 schedule: %w", err)
	}
	return value, true, nil
}

func (repository *SQLiteRepository) UpdateIdempotent(ctx context.Context, request UpdateRequest, key string) (Schedule, bool, error) {
	if request.ID == "" || request.ExpectedRevision < 1 {
		return Schedule{}, false, fmt.Errorf("%w: schedule 更新标识无效", ErrInvalidArgument)
	}
	normalized, err := normalizeRequest(request.CreateRequest)
	if err != nil {
		return Schedule{}, false, err
	}
	fingerprint, err := definitionFingerprint("schedule.update", request.ID, request.ExpectedRevision, normalized)
	if err != nil || !validIdempotencyKey(key) {
		return Schedule{}, false, fmt.Errorf("%w: schedule 更新幂等参数无效", ErrInvalidArgument)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, false, fmt.Errorf("开始幂等更新 schedule: %w", err)
	}
	defer transaction.Rollback()
	if stored, found, readErr := readStoredMutation(ctx, transaction, key); readErr != nil {
		return Schedule{}, false, readErr
	} else if found {
		if err := verifyStoredMutation(stored, "schedule.update", fingerprint, request.ID); err != nil {
			return Schedule{}, false, err
		}
		value, getErr := getSchedule(ctx, transaction, stored.scheduleID, true)
		return value, false, getErr
	}
	current, err := getSchedule(ctx, transaction, request.ID, false)
	if err != nil {
		return Schedule{}, false, err
	}
	if current.Revision != request.ExpectedRevision {
		return Schedule{}, false, ErrConflict
	}
	next := scheduleFromRequest(current.ID, normalized, current.Revision+1, current.CreatedAt, normalized.Now)
	next.LastRunAt = current.LastRunAt
	if err := updateScheduleDefinition(ctx, transaction, current, next); err != nil {
		return Schedule{}, false, err
	}
	if err := recordStoredMutation(ctx, transaction, key, "schedule.update", fingerprint, next.ID, "", next.Revision, normalized.Now); err != nil {
		return Schedule{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Schedule{}, false, fmt.Errorf("提交幂等更新 schedule: %w", err)
	}
	return next, true, nil
}

func (repository *SQLiteRepository) SetEnabledIdempotent(ctx context.Context, scheduleID string, expectedRevision int64, enabled bool, key string, now time.Time) (Schedule, bool, error) {
	fingerprint := scalarFingerprint("schedule.set_enabled", scheduleID, expectedRevision, enabled)
	if scheduleID == "" || expectedRevision < 1 || now.IsZero() || !validIdempotencyKey(key) {
		return Schedule{}, false, fmt.Errorf("%w: schedule 启停幂等参数无效", ErrInvalidArgument)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Schedule{}, false, fmt.Errorf("开始幂等启停 schedule: %w", err)
	}
	defer transaction.Rollback()
	if stored, found, readErr := readStoredMutation(ctx, transaction, key); readErr != nil {
		return Schedule{}, false, readErr
	} else if found {
		if err := verifyStoredMutation(stored, "schedule.set_enabled", fingerprint, scheduleID); err != nil {
			return Schedule{}, false, err
		}
		value, getErr := getSchedule(ctx, transaction, stored.scheduleID, true)
		return value, false, getErr
	}
	current, err := getSchedule(ctx, transaction, scheduleID, false)
	if err != nil {
		return Schedule{}, false, err
	}
	if current.Revision != expectedRevision {
		return Schedule{}, false, ErrConflict
	}
	var nextRun *time.Time
	if enabled {
		spec, parseErr := ParseSpec(current.Kind, current.Expression, current.Timezone)
		if parseErr != nil {
			return Schedule{}, false, parseErr
		}
		nextRun = nonZeroPointer(spec.Next(now.Add(-time.Nanosecond)))
		if nextRun == nil {
			return Schedule{}, false, fmt.Errorf("%w: schedule 已无未来触发点", ErrInvalidArgument)
		}
	}
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_schedules SET enabled = ?, next_run_at = ?, revision = revision + 1, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`, boolInt(enabled), nullableTime(nextRun), formatScheduleTime(now), scheduleID, expectedRevision)
	if err != nil {
		return Schedule{}, false, fmt.Errorf("设置 schedule 启用状态: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Schedule{}, false, ErrConflict
	}
	next, err := getSchedule(ctx, transaction, scheduleID, false)
	if err != nil {
		return Schedule{}, false, err
	}
	if err := recordStoredMutation(ctx, transaction, key, "schedule.set_enabled", fingerprint, scheduleID, "", next.Revision, now); err != nil {
		return Schedule{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Schedule{}, false, fmt.Errorf("提交幂等启停 schedule: %w", err)
	}
	return next, true, nil
}

func (repository *SQLiteRepository) DeleteIdempotent(ctx context.Context, scheduleID string, expectedRevision int64, key string, now time.Time) (DeleteResult, bool, error) {
	fingerprint := scalarFingerprint("schedule.delete", scheduleID, expectedRevision)
	if scheduleID == "" || expectedRevision < 1 || now.IsZero() || !validIdempotencyKey(key) {
		return DeleteResult{}, false, fmt.Errorf("%w: schedule 删除幂等参数无效", ErrInvalidArgument)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return DeleteResult{}, false, fmt.Errorf("开始幂等删除 schedule: %w", err)
	}
	defer transaction.Rollback()
	if stored, found, readErr := readStoredMutation(ctx, transaction, key); readErr != nil {
		return DeleteResult{}, false, readErr
	} else if found {
		if err := verifyStoredMutation(stored, "schedule.delete", fingerprint, scheduleID); err != nil {
			return DeleteResult{}, false, err
		}
		return DeleteResult{ScheduleID: scheduleID, Revision: stored.resultRevision}, false, nil
	}
	current, err := getSchedule(ctx, transaction, scheduleID, false)
	if err != nil {
		return DeleteResult{}, false, err
	}
	if current.Revision != expectedRevision {
		return DeleteResult{}, false, ErrConflict
	}
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_schedules SET enabled = 0, next_run_at = NULL, deleted_at = ?, updated_at = ?, revision = revision + 1
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`, formatScheduleTime(now), formatScheduleTime(now), scheduleID, expectedRevision)
	if err != nil {
		return DeleteResult{}, false, fmt.Errorf("软删除 schedule: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return DeleteResult{}, false, ErrConflict
	}
	revision := expectedRevision + 1
	if err := recordStoredMutation(ctx, transaction, key, "schedule.delete", fingerprint, scheduleID, "", revision, now); err != nil {
		return DeleteResult{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return DeleteResult{}, false, fmt.Errorf("提交幂等删除 schedule: %w", err)
	}
	return DeleteResult{ScheduleID: scheduleID, Revision: revision}, true, nil
}

func (repository *SQLiteRepository) RunNowIdempotent(ctx context.Context, scheduleID string, expectedRevision int64, key string, now time.Time) (RunNowResult, bool, error) {
	fingerprint := scalarFingerprint("schedule.run_now", scheduleID, expectedRevision)
	if repository.tasks == nil || scheduleID == "" || expectedRevision < 1 || now.IsZero() || !validIdempotencyKey(key) {
		return RunNowResult{}, false, fmt.Errorf("%w: schedule 立即运行参数无效", ErrInvalidArgument)
	}
	now = now.UTC()
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return RunNowResult{}, false, fmt.Errorf("开始 schedule 立即运行: %w", err)
	}
	defer transaction.Rollback()
	if stored, found, readErr := readStoredMutation(ctx, transaction, key); readErr != nil {
		return RunNowResult{}, false, readErr
	} else if found {
		if err := verifyStoredMutation(stored, "schedule.run_now", fingerprint, scheduleID); err != nil {
			return RunNowResult{}, false, err
		}
		schedule, getErr := getSchedule(ctx, transaction, scheduleID, true)
		if getErr != nil {
			return RunNowResult{}, false, getErr
		}
		occurrence, getErr := getOccurrence(ctx, transaction, stored.occurrenceID.String)
		return RunNowResult{Schedule: schedule, Occurrence: occurrence}, false, getErr
	}
	current, err := getSchedule(ctx, transaction, scheduleID, false)
	if err != nil {
		return RunNowResult{}, false, err
	}
	if current.Revision != expectedRevision {
		return RunNowResult{}, false, ErrConflict
	}
	template, err := decodeTaskTemplate([]byte(current.TaskTemplateJSON), now)
	if err != nil {
		return RunNowResult{}, false, err
	}
	occurrence, err := repository.applyOccurrence(ctx, transaction, current, template, PlannedOccurrence{PlannedAt: now, ScheduledAt: now}, now)
	if err != nil {
		return RunNowResult{}, false, err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_schedules SET revision = revision + 1, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`, formatScheduleTime(now), scheduleID, expectedRevision)
	if err != nil {
		return RunNowResult{}, false, fmt.Errorf("推进立即运行 schedule: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return RunNowResult{}, false, ErrConflict
	}
	current.Revision++
	current.UpdatedAt = now
	if err := recordStoredMutation(ctx, transaction, key, "schedule.run_now", fingerprint, scheduleID, occurrence.ID, current.Revision, now); err != nil {
		return RunNowResult{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return RunNowResult{}, false, fmt.Errorf("提交 schedule 立即运行: %w", err)
	}
	return RunNowResult{Schedule: current, Occurrence: occurrence}, true, nil
}

func (repository *SQLiteRepository) ListOccurrences(ctx context.Context, request OccurrenceListRequest) (OccurrencePage, error) {
	if request.ScheduleID == "" || request.Limit < 1 || request.Limit > MaxSchedulePage ||
		(request.BeforePlannedAt == nil) != (request.BeforeOccurrenceID == "") {
		return OccurrencePage{}, fmt.Errorf("%w: occurrence 列表参数无效", ErrInvalidArgument)
	}
	if _, err := repository.Get(ctx, request.ScheduleID); err != nil {
		return OccurrencePage{}, err
	}
	query := `SELECT occurrence_id, schedule_id, planned_at, task_id, state, created_at, reason_code
FROM mzfw_schedule_occurrences WHERE schedule_id = ?`
	arguments := []any{request.ScheduleID}
	if request.BeforePlannedAt != nil {
		query += ` AND (planned_at < ? OR (planned_at = ? AND occurrence_id < ?))`
		cursor := formatScheduleTime(*request.BeforePlannedAt)
		arguments = append(arguments, cursor, cursor, request.BeforeOccurrenceID)
	}
	query += ` ORDER BY planned_at DESC, occurrence_id DESC LIMIT ?`
	arguments = append(arguments, request.Limit+1)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return OccurrencePage{}, fmt.Errorf("查询 occurrence 列表: %w", err)
	}
	defer rows.Close()
	values := make([]Occurrence, 0, request.Limit+1)
	for rows.Next() {
		value, scanErr := scanOccurrence(rows)
		if scanErr != nil {
			return OccurrencePage{}, fmt.Errorf("扫描 occurrence 列表: %w", scanErr)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return OccurrencePage{}, fmt.Errorf("遍历 occurrence 列表: %w", err)
	}
	page := OccurrencePage{HasMore: len(values) > request.Limit}
	if page.HasMore {
		values = values[:request.Limit]
	}
	page.Occurrences = values
	if len(values) > 0 {
		last := values[len(values)-1]
		page.NextPlannedAt = &last.PlannedAt
		page.NextOccurrenceID = last.ID
	}
	return page, nil
}

func insertSchedule(ctx context.Context, transaction *sql.Tx, value Schedule) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_schedules (
schedule_id, name, enabled, kind, expression, timezone, misfire_policy, overlap_policy,
jitter_ms, next_run_at, last_run_at, payload_template, revision, max_catch_up,
overlap_limit, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, NULL)`, value.ID, value.Name,
		boolInt(value.Enabled), value.Kind, value.Expression, value.Timezone, value.MisfirePolicy, value.OverlapPolicy,
		value.Jitter.Milliseconds(), nullableTime(value.NextRunAt), value.TaskTemplateJSON, value.Revision, value.MaxCatchUp,
		value.OverlapLimit, formatScheduleTime(value.CreatedAt), formatScheduleTime(value.UpdatedAt))
	if err != nil {
		return fmt.Errorf("创建 schedule: %w", err)
	}
	return nil
}

func updateScheduleDefinition(ctx context.Context, transaction *sql.Tx, current, next Schedule) error {
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_schedules SET name = ?, enabled = ?, kind = ?, expression = ?, timezone = ?, misfire_policy = ?,
overlap_policy = ?, jitter_ms = ?, next_run_at = ?, payload_template = ?, revision = ?, max_catch_up = ?, overlap_limit = ?, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`, next.Name, boolInt(next.Enabled), next.Kind, next.Expression,
		next.Timezone, next.MisfirePolicy, next.OverlapPolicy, next.Jitter.Milliseconds(), nullableTime(next.NextRunAt), next.TaskTemplateJSON,
		next.Revision, next.MaxCatchUp, next.OverlapLimit, formatScheduleTime(next.UpdatedAt), next.ID, current.Revision)
	if err != nil {
		return fmt.Errorf("更新 schedule: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrConflict
	}
	return nil
}

func readStoredMutation(ctx context.Context, transaction *sql.Tx, key string) (storedMutation, bool, error) {
	var value storedMutation
	err := transaction.QueryRowContext(ctx, `SELECT operation, request_fingerprint, schedule_id, occurrence_id, result_revision
FROM mzfw_schedule_idempotency WHERE idempotency_key = ?`, key).Scan(&value.operation, &value.fingerprint, &value.scheduleID, &value.occurrenceID, &value.resultRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return storedMutation{}, false, nil
	}
	if err != nil {
		return storedMutation{}, false, fmt.Errorf("读取 schedule 幂等记录: %w", err)
	}
	return value, true, nil
}

func recordStoredMutation(ctx context.Context, transaction *sql.Tx, key, operation, fingerprint, scheduleID, occurrenceID string, revision int64, now time.Time) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_schedule_idempotency (
idempotency_key, operation, request_fingerprint, schedule_id, occurrence_id, result_revision, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`, key, operation, fingerprint, scheduleID, nullableString(occurrenceID), revision, formatScheduleTime(now))
	if err != nil {
		return fmt.Errorf("记录 schedule 幂等操作: %w", err)
	}
	return nil
}

func verifyStoredMutation(value storedMutation, operation, fingerprint, scheduleID string) error {
	if value.operation != operation || value.fingerprint != fingerprint || (scheduleID != "" && value.scheduleID != scheduleID) {
		return fmt.Errorf("%w: idempotency key 已对应其他 schedule 操作", ErrConflict)
	}
	return nil
}

func definitionFingerprint(operation, scheduleID string, revision int64, request normalizedRequest) (string, error) {
	payload := struct {
		Operation     string          `json:"operation"`
		ScheduleID    string          `json:"schedule_id,omitempty"`
		Revision      int64           `json:"revision,omitempty"`
		Name          string          `json:"name"`
		Enabled       bool            `json:"enabled"`
		Kind          Kind            `json:"kind"`
		Expression    string          `json:"expression"`
		Timezone      string          `json:"timezone"`
		MisfirePolicy MisfirePolicy   `json:"misfire_policy"`
		OverlapPolicy OverlapPolicy   `json:"overlap_policy"`
		JitterMS      int64           `json:"jitter_ms"`
		MaxCatchUp    int             `json:"max_catch_up"`
		OverlapLimit  int             `json:"overlap_limit"`
		TaskTemplate  json.RawMessage `json:"task_template"`
	}{operation, scheduleID, revision, request.Name, request.Enabled, request.Kind, request.Expression, request.Timezone,
		request.MisfirePolicy, request.OverlapPolicy, request.Jitter.Milliseconds(), request.MaxCatchUp, request.OverlapLimit,
		json.RawMessage(request.templateJSON)}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return hashFingerprint(data), nil
}

func scalarFingerprint(values ...any) string {
	data, _ := json.Marshal(values)
	return hashFingerprint(data)
}

func hashFingerprint(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validIdempotencyKey(value string) bool { return idempotencyKeyPattern.MatchString(value) }

type occurrenceScanner interface{ Scan(...any) error }

func getOccurrence(ctx context.Context, transaction *sql.Tx, occurrenceID string) (Occurrence, error) {
	if occurrenceID == "" {
		return Occurrence{}, ErrNotFound
	}
	value, err := scanOccurrence(transaction.QueryRowContext(ctx, `SELECT occurrence_id, schedule_id, planned_at, task_id, state, created_at, reason_code
FROM mzfw_schedule_occurrences WHERE occurrence_id = ?`, occurrenceID))
	if errors.Is(err, sql.ErrNoRows) {
		return Occurrence{}, ErrNotFound
	}
	if err != nil {
		return Occurrence{}, fmt.Errorf("读取 schedule occurrence: %w", err)
	}
	return value, nil
}

func scanOccurrence(row occurrenceScanner) (Occurrence, error) {
	var value Occurrence
	var plannedAt, createdAt string
	var taskID, reason sql.NullString
	if err := row.Scan(&value.ID, &value.ScheduleID, &plannedAt, &taskID, &value.State, &createdAt, &reason); err != nil {
		return Occurrence{}, err
	}
	var err error
	if value.PlannedAt, err = parseScheduleTime(plannedAt); err != nil {
		return Occurrence{}, err
	}
	if value.CreatedAt, err = parseScheduleTime(createdAt); err != nil {
		return Occurrence{}, err
	}
	value.TaskID = taskID.String
	value.ReasonCode = reason.String
	return value, nil
}
