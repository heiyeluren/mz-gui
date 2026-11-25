package scheduler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
)

type IDSource func(prefix string) string

type transactionalTaskStore interface {
	CreateInTransaction(context.Context, *sql.Tx, taskengine.CreateRequest) (taskengine.Task, bool, error)
	RequestCancelInTransaction(context.Context, *sql.Tx, string, time.Time) (taskengine.Task, error)
}

type SQLiteRepository struct {
	database *sql.DB
	tasks    transactionalTaskStore
	newID    IDSource
}

func NewSQLiteRepository(database *sql.DB, tasks transactionalTaskStore, ids IDSource) *SQLiteRepository {
	if ids == nil {
		ids = randomScheduleID
	}
	return &SQLiteRepository{database: database, tasks: tasks, newID: ids}
}

func (repository *SQLiteRepository) Create(ctx context.Context, request CreateRequest) (Schedule, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Schedule{}, err
	}
	schedule := scheduleFromRequest(repository.newID("schedule"), normalized, 1, normalized.Now, normalized.Now)
	if schedule.ID == "" {
		return Schedule{}, fmt.Errorf("%w: schedule id 生成失败", ErrInvalidArgument)
	}
	_, err = repository.database.ExecContext(ctx, `INSERT INTO mzfw_schedules (
schedule_id, name, enabled, kind, expression, timezone, misfire_policy, overlap_policy,
jitter_ms, next_run_at, last_run_at, payload_template, revision, max_catch_up,
overlap_limit, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, NULL)`,
		schedule.ID, schedule.Name, boolInt(schedule.Enabled), schedule.Kind, schedule.Expression,
		schedule.Timezone, schedule.MisfirePolicy, schedule.OverlapPolicy, schedule.Jitter.Milliseconds(),
		nullableTime(schedule.NextRunAt), schedule.TaskTemplateJSON, schedule.Revision, schedule.MaxCatchUp,
		schedule.OverlapLimit, formatScheduleTime(schedule.CreatedAt), formatScheduleTime(schedule.UpdatedAt))
	if err != nil {
		return Schedule{}, fmt.Errorf("创建 schedule: %w", err)
	}
	return schedule, nil
}

func (repository *SQLiteRepository) Get(ctx context.Context, scheduleID string) (Schedule, error) {
	if scheduleID == "" {
		return Schedule{}, fmt.Errorf("%w: schedule_id 不能为空", ErrInvalidArgument)
	}
	return getSchedule(ctx, repository.database, scheduleID, false)
}

func (repository *SQLiteRepository) List(ctx context.Context, request ListRequest) ([]Schedule, error) {
	if request.Limit < 1 || request.Limit > MaxSchedulePage {
		return nil, fmt.Errorf("%w: schedule 列表参数无效", ErrInvalidArgument)
	}
	query := `SELECT ` + scheduleColumns + ` FROM mzfw_schedules WHERE deleted_at IS NULL`
	if !request.IncludeDisabled {
		query += ` AND enabled = 1`
	}
	query += ` ORDER BY updated_at DESC, schedule_id DESC LIMIT ?`
	rows, err := repository.database.QueryContext(ctx, query, request.Limit)
	if err != nil {
		return nil, fmt.Errorf("查询 schedule 列表: %w", err)
	}
	defer rows.Close()
	values := make([]Schedule, 0, request.Limit)
	for rows.Next() {
		value, scanErr := scanSchedule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("扫描 schedule 列表: %w", scanErr)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 schedule 列表: %w", err)
	}
	return values, nil
}

func (repository *SQLiteRepository) Update(ctx context.Context, request UpdateRequest) (Schedule, error) {
	if request.ID == "" || request.ExpectedRevision < 1 {
		return Schedule{}, fmt.Errorf("%w: schedule 更新标识无效", ErrInvalidArgument)
	}
	normalized, err := normalizeRequest(request.CreateRequest)
	if err != nil {
		return Schedule{}, err
	}
	current, err := getSchedule(ctx, repository.database, request.ID, false)
	if err != nil {
		return Schedule{}, err
	}
	if current.Revision != request.ExpectedRevision {
		return Schedule{}, ErrConflict
	}
	next := scheduleFromRequest(current.ID, normalized, current.Revision+1, current.CreatedAt, normalized.Now)
	next.LastRunAt = current.LastRunAt
	result, err := repository.database.ExecContext(ctx, `UPDATE mzfw_schedules SET
name = ?, enabled = ?, kind = ?, expression = ?, timezone = ?, misfire_policy = ?,
overlap_policy = ?, jitter_ms = ?, next_run_at = ?, payload_template = ?, revision = ?,
max_catch_up = ?, overlap_limit = ?, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`,
		next.Name, boolInt(next.Enabled), next.Kind, next.Expression, next.Timezone, next.MisfirePolicy,
		next.OverlapPolicy, next.Jitter.Milliseconds(), nullableTime(next.NextRunAt), next.TaskTemplateJSON,
		next.Revision, next.MaxCatchUp, next.OverlapLimit, formatScheduleTime(next.UpdatedAt), next.ID, current.Revision)
	if err != nil {
		return Schedule{}, fmt.Errorf("更新 schedule: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Schedule{}, ErrConflict
	}
	return next, nil
}

func (repository *SQLiteRepository) SetEnabled(ctx context.Context, scheduleID string, expectedRevision int64, enabled bool, now time.Time) (Schedule, error) {
	if scheduleID == "" || expectedRevision < 1 || now.IsZero() {
		return Schedule{}, fmt.Errorf("%w: schedule 启停参数无效", ErrInvalidArgument)
	}
	current, err := getSchedule(ctx, repository.database, scheduleID, false)
	if err != nil {
		return Schedule{}, err
	}
	if current.Revision != expectedRevision {
		return Schedule{}, ErrConflict
	}
	var nextRun *time.Time
	if enabled {
		spec, parseErr := ParseSpec(current.Kind, current.Expression, current.Timezone)
		if parseErr != nil {
			return Schedule{}, parseErr
		}
		nextRun = nonZeroPointer(spec.Next(now.Add(-time.Nanosecond)))
		if nextRun == nil {
			return Schedule{}, fmt.Errorf("%w: schedule 已无未来触发点", ErrInvalidArgument)
		}
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE mzfw_schedules
SET enabled = ?, next_run_at = ?, revision = revision + 1, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`,
		boolInt(enabled), nullableTime(nextRun), formatScheduleTime(now), scheduleID, expectedRevision)
	if err != nil {
		return Schedule{}, fmt.Errorf("设置 schedule 启用状态: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Schedule{}, ErrConflict
	}
	return getSchedule(ctx, repository.database, scheduleID, false)
}

func (repository *SQLiteRepository) Delete(ctx context.Context, scheduleID string, expectedRevision int64, now time.Time) error {
	if scheduleID == "" || expectedRevision < 1 || now.IsZero() {
		return fmt.Errorf("%w: schedule 删除参数无效", ErrInvalidArgument)
	}
	result, err := repository.database.ExecContext(ctx, `UPDATE mzfw_schedules
SET enabled = 0, next_run_at = NULL, deleted_at = ?, updated_at = ?, revision = revision + 1
WHERE schedule_id = ? AND revision = ? AND deleted_at IS NULL`,
		formatScheduleTime(now), formatScheduleTime(now), scheduleID, expectedRevision)
	if err != nil {
		return fmt.Errorf("软删除 schedule: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		if _, getErr := getSchedule(ctx, repository.database, scheduleID, true); errors.Is(getErr, ErrNotFound) {
			return getErr
		}
		return ErrConflict
	}
	return nil
}

func (repository *SQLiteRepository) Due(ctx context.Context, now time.Time, limit int) ([]Schedule, error) {
	if now.IsZero() || limit < 1 || limit > MaxSchedulePage {
		return nil, fmt.Errorf("%w: due 查询参数无效", ErrInvalidArgument)
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM mzfw_schedules
WHERE enabled = 1 AND deleted_at IS NULL AND next_run_at IS NOT NULL AND next_run_at <= ?
ORDER BY next_run_at ASC, schedule_id ASC LIMIT ?`, formatScheduleTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("查询到期 schedule: %w", err)
	}
	defer rows.Close()
	values := make([]Schedule, 0, limit)
	for rows.Next() {
		value, scanErr := scanSchedule(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("扫描到期 schedule: %w", scanErr)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历到期 schedule: %w", err)
	}
	return values, nil
}

func (repository *SQLiteRepository) ApplyPlan(ctx context.Context, snapshot Schedule, plan Plan, now time.Time) ([]Occurrence, error) {
	if repository.tasks == nil || snapshot.ID == "" || snapshot.Revision < 1 || now.IsZero() || len(plan.Occurrences) > maximumPlannedOccurrences {
		return nil, fmt.Errorf("%w: schedule plan 无效", ErrInvalidArgument)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("开始 schedule plan 事务: %w", err)
	}
	defer transaction.Rollback()
	current, err := getSchedule(ctx, transaction, snapshot.ID, false)
	if err != nil {
		return nil, err
	}
	if current.Revision != snapshot.Revision || !current.Enabled {
		return nil, ErrConflict
	}
	template, err := decodeTaskTemplate([]byte(current.TaskTemplateJSON), now)
	if err != nil {
		return nil, err
	}
	result := make([]Occurrence, 0, len(plan.Occurrences))
	for _, planned := range plan.Occurrences {
		occurrence, applyErr := repository.applyOccurrence(ctx, transaction, current, template, planned, now)
		if applyErr != nil {
			return nil, applyErr
		}
		result = append(result, occurrence)
	}
	var lastRun *time.Time
	if len(plan.Occurrences) > 0 {
		value := plan.Occurrences[len(plan.Occurrences)-1].PlannedAt.UTC()
		lastRun = &value
	} else {
		lastRun = current.LastRunAt
	}
	update, err := transaction.ExecContext(ctx, `UPDATE mzfw_schedules SET
next_run_at = ?, last_run_at = ?, revision = revision + 1, updated_at = ?
WHERE schedule_id = ? AND revision = ? AND enabled = 1 AND deleted_at IS NULL`,
		nullableTime(plan.NextRunAt), nullableTime(lastRun), formatScheduleTime(now), current.ID, current.Revision)
	if err != nil {
		return nil, fmt.Errorf("推进 schedule: %w", err)
	}
	if affected, _ := update.RowsAffected(); affected != 1 {
		return nil, ErrConflict
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("提交 schedule plan: %w", err)
	}
	return result, nil
}

func (repository *SQLiteRepository) applyOccurrence(
	ctx context.Context,
	transaction *sql.Tx,
	schedule Schedule,
	template TaskTemplate,
	planned PlannedOccurrence,
	now time.Time,
) (Occurrence, error) {
	if planned.PlannedAt.IsZero() || planned.ScheduledAt.Before(planned.PlannedAt) || planned.ScheduledAt.Sub(planned.PlannedAt) > schedule.Jitter {
		return Occurrence{}, fmt.Errorf("%w: occurrence 时间无效", ErrInvalidArgument)
	}
	occurrence := Occurrence{
		ID: occurrenceID(schedule.ID, planned.PlannedAt), ScheduleID: schedule.ID,
		PlannedAt: planned.PlannedAt.UTC(), State: "created", CreatedAt: now.UTC(),
	}
	activeIDs, err := activeTaskIDs(ctx, transaction, schedule.ID)
	if err != nil {
		return Occurrence{}, err
	}
	switch schedule.OverlapPolicy {
	case OverlapForbid:
		if len(activeIDs) > 0 {
			occurrence.State = "skipped"
			occurrence.ReasonCode = "OVERLAP_FORBIDDEN"
		}
	case OverlapAllowLimited:
		if len(activeIDs) >= schedule.OverlapLimit {
			occurrence.State = "skipped"
			occurrence.ReasonCode = "OVERLAP_LIMIT_REACHED"
		}
	case OverlapReplace:
		for _, taskID := range activeIDs {
			if _, err := repository.tasks.RequestCancelInTransaction(ctx, transaction, taskID, now); err != nil {
				return Occurrence{}, fmt.Errorf("替换调度取消旧任务: %w", err)
			}
		}
	default:
		return Occurrence{}, fmt.Errorf("%w: overlap policy 无效", ErrInvalidArgument)
	}
	if occurrence.State == "created" {
		idempotencyKey := occurrenceIdempotencyKey(schedule.ID, planned.PlannedAt)
		scheduledAt := planned.ScheduledAt.UTC()
		task, _, err := repository.tasks.CreateInTransaction(ctx, transaction, taskengine.CreateRequest{
			Type: template.TaskType, Priority: template.Priority, PayloadVersion: template.PayloadVersion,
			Payload: template.Payload, IdempotencyKey: idempotencyKey, WorkspaceID: template.WorkspaceID,
			RequiredCapabilities: template.RequiredCapabilities, ScheduledAt: &scheduledAt,
			MaxAttempts: template.MaxAttempts, Now: now,
		})
		if err != nil {
			return Occurrence{}, fmt.Errorf("创建 occurrence task: %w", err)
		}
		occurrence.TaskID = task.ID
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO mzfw_schedule_occurrences (
occurrence_id, schedule_id, planned_at, task_id, state, created_at, reason_code
) VALUES (?, ?, ?, ?, ?, ?, ?)`, occurrence.ID, occurrence.ScheduleID, formatScheduleTime(occurrence.PlannedAt),
		nullableString(occurrence.TaskID), occurrence.State, formatScheduleTime(occurrence.CreatedAt), nullableString(occurrence.ReasonCode))
	if err != nil {
		return Occurrence{}, fmt.Errorf("记录 schedule occurrence: %w", err)
	}
	return occurrence, nil
}

type normalizedRequest struct {
	CreateRequest
	templateJSON string
	nextRunAt    *time.Time
}

func normalizeRequest(request CreateRequest) (normalizedRequest, error) {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 128 || strings.ContainsRune(request.Name, '\x00') ||
		request.Now.IsZero() || request.Jitter < 0 || request.Jitter > time.Hour ||
		request.MaxCatchUp < 1 || request.MaxCatchUp > maximumPlannedOccurrences ||
		request.OverlapLimit < 1 || request.OverlapLimit > 16 ||
		!validMisfire(request.MisfirePolicy) || !validOverlap(request.OverlapPolicy) {
		return normalizedRequest{}, fmt.Errorf("%w: schedule 字段无效", ErrInvalidArgument)
	}
	spec, err := ParseSpec(request.Kind, request.Expression, request.Timezone)
	if err != nil {
		return normalizedRequest{}, err
	}
	template, err := decodeTaskTemplate(request.TaskTemplate, request.Now)
	if err != nil {
		return normalizedRequest{}, err
	}
	templateJSON, _ := json.Marshal(template)
	request.Now = request.Now.UTC()
	var nextRun *time.Time
	if request.Enabled {
		nextRun = nonZeroPointer(spec.Next(request.Now.Add(-time.Nanosecond)))
		if nextRun == nil {
			return normalizedRequest{}, fmt.Errorf("%w: schedule 没有未来触发点", ErrInvalidArgument)
		}
	}
	return normalizedRequest{CreateRequest: request, templateJSON: string(templateJSON), nextRunAt: nextRun}, nil
}

func decodeTaskTemplate(data []byte, now time.Time) (TaskTemplate, error) {
	if len(data) == 0 || len(data) > taskengine.MaxPayloadBytes {
		return TaskTemplate{}, fmt.Errorf("%w: task template 大小无效", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var template TaskTemplate
	if err := decoder.Decode(&template); err != nil {
		return TaskTemplate{}, fmt.Errorf("%w: task template JSON 无效", ErrInvalidArgument)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return TaskTemplate{}, fmt.Errorf("%w: task template JSON 无效", ErrInvalidArgument)
	}
	if template.Payload == nil {
		template.Payload = json.RawMessage(`{}`)
	}
	if err := taskengine.ValidateCreateRequest(taskengine.CreateRequest{
		Type: template.TaskType, Priority: template.Priority, PayloadVersion: template.PayloadVersion,
		Payload: template.Payload, IdempotencyKey: "schedule-validation", WorkspaceID: template.WorkspaceID,
		RequiredCapabilities: template.RequiredCapabilities, MaxAttempts: template.MaxAttempts, Now: now,
	}); err != nil {
		return TaskTemplate{}, fmt.Errorf("%w: task template 字段无效", ErrInvalidArgument)
	}
	template.RequiredCapabilities = sortedUnique(template.RequiredCapabilities)
	var payload any
	_ = json.Unmarshal(template.Payload, &payload)
	template.Payload, _ = json.Marshal(payload)
	return template, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("存在多余 JSON")
		}
		return err
	}
	return nil
}

func scheduleFromRequest(id string, request normalizedRequest, revision int64, createdAt, updatedAt time.Time) Schedule {
	return Schedule{
		ID: id, Name: request.Name, Enabled: request.Enabled, Kind: request.Kind,
		Expression: request.Expression, Timezone: request.Timezone, MisfirePolicy: request.MisfirePolicy,
		OverlapPolicy: request.OverlapPolicy, Jitter: request.Jitter, MaxCatchUp: request.MaxCatchUp,
		OverlapLimit: request.OverlapLimit, NextRunAt: request.nextRunAt, TaskTemplateJSON: request.templateJSON,
		Revision: revision, CreatedAt: createdAt.UTC(), UpdatedAt: updatedAt.UTC(),
	}
}

const scheduleColumns = `schedule_id, name, enabled, kind, expression, timezone,
misfire_policy, overlap_policy, jitter_ms, next_run_at, last_run_at, payload_template,
revision, max_catch_up, overlap_limit, created_at, updated_at, deleted_at`

type rowScanner interface{ Scan(...any) error }
type scheduleQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getSchedule(ctx context.Context, query scheduleQuerier, scheduleID string, includeDeleted bool) (Schedule, error) {
	statement := `SELECT ` + scheduleColumns + ` FROM mzfw_schedules WHERE schedule_id = ?`
	if !includeDeleted {
		statement += ` AND deleted_at IS NULL`
	}
	value, err := scanSchedule(query.QueryRowContext(ctx, statement, scheduleID))
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("读取 schedule: %w", err)
	}
	return value, nil
}

func scanSchedule(row rowScanner) (Schedule, error) {
	var value Schedule
	var enabled int
	var kind, misfire, overlap string
	var jitterMS int64
	var nextRun, lastRun, deletedAt sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(&value.ID, &value.Name, &enabled, &kind, &value.Expression, &value.Timezone,
		&misfire, &overlap, &jitterMS, &nextRun, &lastRun, &value.TaskTemplateJSON,
		&value.Revision, &value.MaxCatchUp, &value.OverlapLimit, &createdAt, &updatedAt, &deletedAt)
	if err != nil {
		return Schedule{}, err
	}
	value.Enabled = enabled == 1
	value.Kind = Kind(kind)
	value.MisfirePolicy = MisfirePolicy(misfire)
	value.OverlapPolicy = OverlapPolicy(overlap)
	value.Jitter = time.Duration(jitterMS) * time.Millisecond
	if value.CreatedAt, err = parseScheduleTime(createdAt); err != nil {
		return Schedule{}, err
	}
	if value.UpdatedAt, err = parseScheduleTime(updatedAt); err != nil {
		return Schedule{}, err
	}
	for _, item := range []struct {
		source sql.NullString
		target **time.Time
	}{{nextRun, &value.NextRunAt}, {lastRun, &value.LastRunAt}, {deletedAt, &value.DeletedAt}} {
		if item.source.Valid {
			parsed, parseErr := parseScheduleTime(item.source.String)
			if parseErr != nil {
				return Schedule{}, parseErr
			}
			*item.target = &parsed
		}
	}
	return value, nil
}

func activeTaskIDs(ctx context.Context, transaction *sql.Tx, scheduleID string) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT t.task_id
FROM mzfw_schedule_occurrences o JOIN mzfw_tasks t ON t.task_id = o.task_id
WHERE o.schedule_id = ? AND t.state IN (?, ?, ?, ?)
ORDER BY o.planned_at ASC, t.task_id ASC`, scheduleID, taskengine.StatePending, taskengine.StateLeased, taskengine.StateRunning, taskengine.StateRetryWait)
	if err != nil {
		return nil, fmt.Errorf("查询 schedule 活动任务: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("扫描 schedule 活动任务: %w", err)
		}
		ids = append(ids, taskID)
	}
	return ids, rows.Err()
}

func validMisfire(policy MisfirePolicy) bool {
	return policy == MisfireSkip || policy == MisfireRunOnce || policy == MisfireCatchUpLimited
}

func validOverlap(policy OverlapPolicy) bool {
	return policy == OverlapForbid || policy == OverlapAllowLimited || policy == OverlapReplace
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func occurrenceID(scheduleID string, plannedAt time.Time) string {
	digest := sha256.Sum256([]byte(scheduleID + "\x00" + plannedAt.UTC().Format(time.RFC3339Nano)))
	return "occ_" + hex.EncodeToString(digest[:16])
}

func occurrenceIdempotencyKey(scheduleID string, plannedAt time.Time) string {
	digest := sha256.Sum256([]byte("schedule\x00" + scheduleID + "\x00" + plannedAt.UTC().Format(time.RFC3339Nano)))
	return "schedule_occurrence:" + hex.EncodeToString(digest[:])
}

func randomScheduleID(prefix string) string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatScheduleTime(*value)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func formatScheduleTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseScheduleTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("schedule 时间为空")
	}
	return time.Parse(time.RFC3339Nano, value)
}
