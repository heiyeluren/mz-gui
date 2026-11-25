package taskengine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]*$`)

type IDSource func(prefix string) string

type SQLiteRepositoryOptions struct {
	IDSource             IDSource
	RecoveryErrorCode    string
	RecoveryErrorSummary string
}

type SQLiteRepository struct {
	database             *sql.DB
	newID                IDSource
	recoveryErrorCode    string
	recoveryErrorSummary string
}

func NewSQLiteRepository(database *sql.DB, ids IDSource) *SQLiteRepository {
	return NewSQLiteRepositoryWithOptions(database, SQLiteRepositoryOptions{IDSource: ids})
}

func NewSQLiteRepositoryWithOptions(database *sql.DB, options SQLiteRepositoryOptions) *SQLiteRepository {
	if options.IDSource == nil {
		options.IDSource = randomID
	}
	if options.RecoveryErrorCode == "" {
		options.RecoveryErrorCode = "WORKER_INTERRUPTED"
	}
	if options.RecoveryErrorSummary == "" {
		options.RecoveryErrorSummary = "任务工作进程在执行期间停止"
	}
	return &SQLiteRepository{
		database: database, newID: options.IDSource,
		recoveryErrorCode: options.RecoveryErrorCode, recoveryErrorSummary: options.RecoveryErrorSummary,
	}
}

func (repository *SQLiteRepository) Create(ctx context.Context, request CreateRequest) (Task, bool, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, fmt.Errorf("开始创建任务事务: %w", err)
	}
	defer transaction.Rollback()
	task, created, err := repository.CreateInTransaction(ctx, transaction, request)
	if err != nil {
		return Task{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return Task{}, false, fmt.Errorf("提交创建任务: %w", err)
	}
	return task, created, nil
}

// CreateInTransaction 供 occurrence 等跨领域原子事务复用；事务提交权属于调用方。
func (repository *SQLiteRepository) CreateInTransaction(ctx context.Context, transaction *sql.Tx, request CreateRequest) (Task, bool, error) {
	if transaction == nil {
		return Task{}, false, fmt.Errorf("%w: 任务事务不能为空", ErrInvalidArgument)
	}
	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		return Task{}, false, err
	}
	existing, err := getTaskByIdempotencyKey(ctx, transaction, normalized.IdempotencyKey)
	if err == nil {
		if !sameCreateSemantics(existing, normalized) {
			return Task{}, false, fmt.Errorf("%w: idempotency key 已对应其他请求", ErrConflict)
		}
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Task{}, false, err
	}

	task := Task{
		ID:                       repository.newID("task"),
		Type:                     normalized.Type,
		State:                    StatePending,
		Priority:                 normalized.Priority,
		PayloadVersion:           normalized.PayloadVersion,
		PayloadJSON:              string(normalized.Payload),
		IdempotencyKey:           normalized.IdempotencyKey,
		WorkspaceID:              normalized.WorkspaceID,
		RequiredCapabilitiesJSON: normalized.capabilitiesJSON,
		CreatedAt:                normalized.Now.UTC(),
		ScheduledAt:              normalized.ScheduledAt,
		MaxAttempts:              normalized.MaxAttempts,
		Revision:                 1,
	}
	if task.ID == "" {
		return Task{}, false, fmt.Errorf("%w: task id 生成失败", ErrPrecondition)
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO mzfw_tasks (
task_id, task_type, state, priority, payload_version, payload_json, idempotency_key,
workspace_id, required_capabilities_json, created_at, scheduled_at, attempt_count,
max_attempts, revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		task.ID, task.Type, task.State, task.Priority, task.PayloadVersion, task.PayloadJSON,
		task.IdempotencyKey, nullableString(task.WorkspaceID), task.RequiredCapabilitiesJSON,
		formatTime(task.CreatedAt), nullableTime(task.ScheduledAt), task.MaxAttempts, task.Revision)
	if err != nil {
		return Task{}, false, fmt.Errorf("插入任务: %w", err)
	}
	return task, true, nil
}

func ValidateCreateRequest(request CreateRequest) error {
	_, err := normalizeCreateRequest(request)
	return err
}

func (repository *SQLiteRepository) Get(ctx context.Context, taskID string) (Task, error) {
	if taskID == "" {
		return Task{}, fmt.Errorf("%w: task_id 不能为空", ErrInvalidArgument)
	}
	return getTaskByID(ctx, repository.database, taskID)
}

func (repository *SQLiteRepository) List(ctx context.Context, request ListRequest) (TaskPage, error) {
	states, types, err := normalizeListRequest(request)
	if err != nil {
		return TaskPage{}, err
	}
	conditions := make([]string, 0, 4)
	arguments := make([]any, 0, len(states)+len(types)+6)
	if len(states) > 0 {
		conditions = append(conditions, "state IN ("+placeholders(len(states))+")")
		for _, state := range states {
			arguments = append(arguments, state)
		}
	}
	if len(types) > 0 {
		conditions = append(conditions, "task_type IN ("+placeholders(len(types))+")")
		for _, taskType := range types {
			arguments = append(arguments, taskType)
		}
	}
	if request.WorkspaceID != "" {
		conditions = append(conditions, "workspace_id = ?")
		arguments = append(arguments, request.WorkspaceID)
	}
	if request.BeforeCreatedAt != nil {
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND task_id < ?))")
		cursorTime := formatTime(*request.BeforeCreatedAt)
		arguments = append(arguments, cursorTime, cursorTime, request.BeforeTaskID)
	}
	query := "SELECT " + taskColumns + " FROM mzfw_tasks"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, task_id DESC LIMIT ?"
	arguments = append(arguments, request.Limit+1)
	rows, err := repository.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return TaskPage{}, fmt.Errorf("查询任务列表: %w", err)
	}
	defer rows.Close()
	tasks := make([]Task, 0, request.Limit+1)
	for rows.Next() {
		task, scanErr := scanTask(rows)
		if scanErr != nil {
			return TaskPage{}, fmt.Errorf("扫描任务列表: %w", scanErr)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return TaskPage{}, fmt.Errorf("遍历任务列表: %w", err)
	}
	page := TaskPage{HasMore: len(tasks) > request.Limit}
	if page.HasMore {
		tasks = tasks[:request.Limit]
	}
	page.Tasks = tasks
	if len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		createdAt := last.CreatedAt
		page.NextCreatedAt = &createdAt
		page.NextTaskID = last.ID
	}
	return page, nil
}

func (repository *SQLiteRepository) Retry(ctx context.Context, taskID string, now time.Time) (Task, error) {
	if taskID == "" || now.IsZero() {
		return Task{}, fmt.Errorf("%w: 人工重试参数无效", ErrInvalidArgument)
	}
	return repository.transition(ctx, taskID, Command{Kind: CommandManualRetry, Now: now})
}

func (repository *SQLiteRepository) RetryIdempotent(ctx context.Context, taskID, reason, idempotencyKey string, now time.Time) (Task, bool, error) {
	return repository.idempotentTransition(ctx, "task.retry", taskID, reason, idempotencyKey, Command{Kind: CommandManualRetry, Now: now})
}

func (repository *SQLiteRepository) LeaseNext(ctx context.Context, request LeaseRequest) (Task, bool, error) {
	if request.Owner == "" || len(request.Owner) > 128 || request.Now.IsZero() || request.Duration < 100*time.Millisecond || request.Duration > time.Hour {
		return Task{}, false, fmt.Errorf("%w: lease 请求无效", ErrInvalidArgument)
	}
	leaseUntil := request.Now.Add(request.Duration)
	row := repository.database.QueryRowContext(ctx, `UPDATE mzfw_tasks
SET state = ?, lease_owner = ?, lease_expires_at = ?, revision = revision + 1
WHERE task_id = (
    SELECT task_id FROM mzfw_tasks
    WHERE state = ? AND (scheduled_at IS NULL OR scheduled_at <= ?)
    ORDER BY priority DESC, created_at ASC, task_id ASC
    LIMIT 1
)
RETURNING `+taskColumns,
		StateLeased, request.Owner, formatTime(leaseUntil), StatePending, formatTime(request.Now))
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, fmt.Errorf("领取任务: %w", err)
	}
	return task, true, nil
}

func (repository *SQLiteRepository) Start(ctx context.Context, taskID, owner string, now time.Time) (Task, Attempt, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, Attempt{}, fmt.Errorf("开始任务启动事务: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return Task{}, Attempt{}, err
	}
	next, err := Apply(current, Command{Kind: CommandStart, Now: now, LeaseOwner: owner})
	if err != nil {
		return Task{}, Attempt{}, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return Task{}, Attempt{}, err
	}
	startCursor, err := nextOutputCursor(ctx, transaction, taskID)
	if err != nil {
		return Task{}, Attempt{}, err
	}
	attempt := Attempt{
		ID: repository.newID("attempt"), TaskID: taskID, Number: next.AttemptCount,
		WorkerID: owner, StartedAt: now.UTC(), OutputStartCursor: startCursor,
	}
	if attempt.ID == "" {
		return Task{}, Attempt{}, fmt.Errorf("%w: attempt id 生成失败", ErrPrecondition)
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO mzfw_task_attempts (
attempt_id, task_id, attempt_no, worker_id, started_at, output_start_cursor
) VALUES (?, ?, ?, ?, ?, ?)`, attempt.ID, attempt.TaskID, attempt.Number, attempt.WorkerID, formatTime(attempt.StartedAt), attempt.OutputStartCursor)
	if err != nil {
		return Task{}, Attempt{}, fmt.Errorf("创建 task attempt: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Task{}, Attempt{}, fmt.Errorf("提交任务启动: %w", err)
	}
	return next, attempt, nil
}

func (repository *SQLiteRepository) RenewLease(ctx context.Context, taskID, owner string, now time.Time, duration time.Duration) (Task, error) {
	if taskID == "" || owner == "" || now.IsZero() || duration < 100*time.Millisecond || duration > time.Hour {
		return Task{}, fmt.Errorf("%w: 续租参数无效", ErrInvalidArgument)
	}
	return repository.transition(ctx, taskID, Command{
		Kind: CommandRenewLease, Now: now, LeaseOwner: owner, LeaseUntil: now.Add(duration),
	})
}

func (repository *SQLiteRepository) RequestCancel(ctx context.Context, taskID string, now time.Time) (Task, error) {
	return repository.transition(ctx, taskID, Command{Kind: CommandRequestCancel, Now: now})
}

// RequestCancelInTransaction 在调用方事务内按状态机请求取消。
func (repository *SQLiteRepository) RequestCancelInTransaction(ctx context.Context, transaction *sql.Tx, taskID string, now time.Time) (Task, error) {
	if transaction == nil || taskID == "" || now.IsZero() {
		return Task{}, fmt.Errorf("%w: 事务取消参数无效", ErrInvalidArgument)
	}
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return Task{}, err
	}
	next, err := Apply(current, Command{Kind: CommandRequestCancel, Now: now})
	if err != nil {
		return Task{}, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return Task{}, err
	}
	return next, nil
}

func (repository *SQLiteRepository) RequestCancelIdempotent(ctx context.Context, taskID, reason, idempotencyKey string, now time.Time) (Task, bool, error) {
	return repository.idempotentTransition(ctx, "task.cancel", taskID, reason, idempotencyKey, Command{Kind: CommandRequestCancel, Now: now})
}

func (repository *SQLiteRepository) Finish(ctx context.Context, taskID string, command Command) (Task, error) {
	if command.Kind != CommandSucceed && command.Kind != CommandFail && command.Kind != CommandInterrupt && command.Kind != CommandReconcileSuccess {
		return Task{}, fmt.Errorf("%w: finish 命令无效", ErrInvalidArgument)
	}
	if len(command.ResultSummary) > MaxResultBytes || len(command.ErrorSummary) > MaxResultBytes {
		return Task{}, fmt.Errorf("%w: 任务结果超过上限", ErrResourceExhausted)
	}
	if command.ResultSummary != "" {
		var value any
		if err := json.Unmarshal([]byte(command.ResultSummary), &value); err != nil {
			return Task{}, fmt.Errorf("%w: 结果摘要不是有效 JSON", ErrInvalidArgument)
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return Task{}, fmt.Errorf("%w: 结果摘要无法序列化", ErrInvalidArgument)
		}
		command.ResultSummary = string(canonical)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("开始任务完成事务: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return Task{}, err
	}
	next, err := Apply(current, command)
	if err != nil {
		return Task{}, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return Task{}, err
	}
	endCursor, err := currentOutputCursor(ctx, transaction, taskID)
	if err != nil {
		return Task{}, err
	}
	outcome := string(next.State)
	if next.State == StateRetryWait {
		outcome = string(StateFailed)
	}
	attemptCondition := " AND finished_at IS NULL"
	if command.Kind == CommandReconcileSuccess {
		// 中断恢复已经结束 attempt；回执校验后以最终事实替换其中断结果。
		attemptCondition = ""
	}
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_task_attempts
SET finished_at = ?, outcome = ?, error_code = ?, error_summary = ?, output_end_cursor = ?
WHERE task_id = ? AND attempt_no = ?`+attemptCondition,
		formatTime(command.Now), outcome, nullableString(command.ErrorCode), nullableString(command.ErrorSummary),
		endCursor, taskID, current.AttemptCount)
	if err != nil {
		return Task{}, fmt.Errorf("结束 task attempt: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Task{}, fmt.Errorf("%w: 当前 attempt 不存在", ErrConflict)
	}
	if err := transaction.Commit(); err != nil {
		return Task{}, fmt.Errorf("提交任务完成: %w", err)
	}
	return next, nil
}

func (repository *SQLiteRepository) AppendOutput(ctx context.Context, taskID, kind string, payload []byte, now time.Time) (Output, error) {
	if taskID == "" || !identifierPattern.MatchString(kind) || now.IsZero() {
		return Output{}, fmt.Errorf("%w: 输出参数无效", ErrInvalidArgument)
	}
	if len(payload) == 0 || len(payload) > MaxOutputChunkBytes {
		return Output{}, fmt.Errorf("%w: 输出块超过上限", ErrResourceExhausted)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Output{}, fmt.Errorf("开始输出事务: %w", err)
	}
	defer transaction.Rollback()
	if _, err := getTaskByID(ctx, transaction, taskID); err != nil {
		return Output{}, err
	}
	cursor, err := nextOutputCursor(ctx, transaction, taskID)
	if err != nil {
		return Output{}, err
	}
	output := Output{TaskID: taskID, Cursor: cursor, Kind: kind, OccurredAt: now.UTC(), Payload: append([]byte(nil), payload...)}
	_, err = transaction.ExecContext(ctx, `INSERT INTO mzfw_task_output(task_id, cursor, kind, occurred_at, payload)
VALUES (?, ?, ?, ?, ?)`, output.TaskID, output.Cursor, output.Kind, formatTime(output.OccurredAt), output.Payload)
	if err != nil {
		return Output{}, fmt.Errorf("写入任务输出: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Output{}, fmt.Errorf("提交任务输出: %w", err)
	}
	return output, nil
}

func (repository *SQLiteRepository) ReadOutput(ctx context.Context, taskID string, afterCursor int64, limit int) ([]Output, error) {
	if taskID == "" || afterCursor < 0 || limit < 1 || limit > MaxOutputPage {
		return nil, fmt.Errorf("%w: 输出分页参数无效", ErrInvalidArgument)
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT task_id, cursor, kind, occurred_at, payload
FROM mzfw_task_output WHERE task_id = ? AND cursor > ? ORDER BY cursor ASC LIMIT ?`, taskID, afterCursor, limit)
	if err != nil {
		return nil, fmt.Errorf("读取任务输出: %w", err)
	}
	defer rows.Close()
	outputs := make([]Output, 0, limit)
	for rows.Next() {
		var output Output
		var occurredAt string
		if err := rows.Scan(&output.TaskID, &output.Cursor, &output.Kind, &occurredAt, &output.Payload); err != nil {
			return nil, fmt.Errorf("扫描任务输出: %w", err)
		}
		output.OccurredAt, err = parseTime(occurredAt)
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, output)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历任务输出: %w", err)
	}
	return outputs, nil
}

func (repository *SQLiteRepository) SaveCheckpoint(ctx context.Context, request CheckpointRequest) (Checkpoint, bool, error) {
	normalized, err := normalizeCheckpointRequest(request)
	if err != nil {
		return Checkpoint{}, false, err
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("开始 checkpoint 事务: %w", err)
	}
	defer transaction.Rollback()
	task, err := getTaskByID(ctx, transaction, normalized.TaskID)
	if err != nil {
		return Checkpoint{}, false, err
	}
	if task.State != StateRunning || task.AttemptCount != normalized.AttemptNo {
		return Checkpoint{}, false, fmt.Errorf("%w: checkpoint 只允许当前 running attempt 写入", ErrPrecondition)
	}
	existing, found, err := getCheckpointByKey(ctx, transaction, normalized.TaskID, normalized.Key)
	if err != nil {
		return Checkpoint{}, false, err
	}
	if found {
		if existing.PayloadJSON != string(normalized.Payload) {
			return Checkpoint{}, false, fmt.Errorf("%w: checkpoint key 已对应其他内容", ErrConflict)
		}
		return existing, false, nil
	}

	var revision int64
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(MAX(checkpoint_revision), 0) + 1
FROM mzfw_task_checkpoints WHERE task_id = ?`, normalized.TaskID).Scan(&revision); err != nil {
		return Checkpoint{}, false, fmt.Errorf("分配 checkpoint revision: %w", err)
	}
	cursor, err := nextOutputCursor(ctx, transaction, normalized.TaskID)
	if err != nil {
		return Checkpoint{}, false, err
	}
	checkpoint := Checkpoint{
		TaskID: normalized.TaskID, Key: normalized.Key, Revision: revision,
		AttemptNo: normalized.AttemptNo, OutputCursor: cursor,
		PayloadJSON: string(normalized.Payload), CreatedAt: normalized.Now,
	}
	envelope, err := json.Marshal(struct {
		Key      string          `json:"key"`
		Revision int64           `json:"revision"`
		Payload  json.RawMessage `json:"payload"`
	}{Key: checkpoint.Key, Revision: checkpoint.Revision, Payload: normalized.Payload})
	if err != nil || len(envelope) > MaxOutputChunkBytes {
		return Checkpoint{}, false, fmt.Errorf("%w: checkpoint 进度事件超过上限", ErrResourceExhausted)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_task_output(task_id, cursor, kind, occurred_at, payload)
VALUES (?, ?, 'checkpoint', ?, ?)`, checkpoint.TaskID, checkpoint.OutputCursor, formatTime(checkpoint.CreatedAt), envelope); err != nil {
		return Checkpoint{}, false, fmt.Errorf("写入 checkpoint 进度: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO mzfw_task_checkpoints(
task_id, checkpoint_key, checkpoint_revision, attempt_no, output_cursor, payload_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`, checkpoint.TaskID, checkpoint.Key, checkpoint.Revision,
		checkpoint.AttemptNo, checkpoint.OutputCursor, checkpoint.PayloadJSON, formatTime(checkpoint.CreatedAt)); err != nil {
		return Checkpoint{}, false, fmt.Errorf("写入 task checkpoint: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Checkpoint{}, false, fmt.Errorf("提交 task checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func (repository *SQLiteRepository) LatestCheckpoint(ctx context.Context, taskID string) (Checkpoint, bool, error) {
	if taskID == "" || !identifierPattern.MatchString(taskID) || len(taskID) > 128 {
		return Checkpoint{}, false, fmt.Errorf("%w: checkpoint task_id 无效", ErrInvalidArgument)
	}
	if _, err := getTaskByID(ctx, repository.database, taskID); err != nil {
		return Checkpoint{}, false, err
	}
	checkpoint, err := scanCheckpoint(repository.database.QueryRowContext(ctx, `SELECT
task_id, checkpoint_key, checkpoint_revision, attempt_no, output_cursor, payload_json, created_at
FROM mzfw_task_checkpoints WHERE task_id = ? ORDER BY checkpoint_revision DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("读取最新 task checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func (repository *SQLiteRepository) PromoteDueRetries(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() || limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("%w: 重试提升参数无效", ErrInvalidArgument)
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT task_id FROM mzfw_tasks
WHERE state = ? AND next_retry_at <= ? ORDER BY next_retry_at ASC, task_id ASC LIMIT ?`,
		StateRetryWait, formatTime(now), limit)
	if err != nil {
		return 0, fmt.Errorf("查询到期重试: %w", err)
	}
	ids, err := collectTaskIDs(rows)
	if err != nil {
		return 0, err
	}
	promoted := 0
	for _, taskID := range ids {
		if _, err := repository.transition(ctx, taskID, Command{Kind: CommandRetryDue, Now: now}); err != nil {
			if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrPrecondition) {
				continue
			}
			return promoted, err
		}
		promoted++
	}
	return promoted, nil
}

func (repository *SQLiteRepository) RecoverExpired(ctx context.Context, now time.Time, limit int, policy RecoveryPolicy) (int, error) {
	if now.IsZero() || limit < 1 || limit > 1000 || policy == nil {
		return 0, fmt.Errorf("%w: 恢复参数无效", ErrInvalidArgument)
	}
	rows, err := repository.database.QueryContext(ctx, `SELECT task_id FROM mzfw_tasks
WHERE (state IN (?, ?) AND lease_expires_at <= ?) OR state = ?
ORDER BY COALESCE(lease_expires_at, created_at) ASC, task_id ASC LIMIT ?`,
		StateLeased, StateRunning, formatTime(now), StateInterrupted, limit)
	if err != nil {
		return 0, fmt.Errorf("查询过期租约: %w", err)
	}
	ids, err := collectTaskIDs(rows)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, taskID := range ids {
		changed, err := repository.recoverOne(ctx, taskID, now, policy)
		if err != nil {
			return recovered, err
		}
		if changed {
			recovered++
		}
	}
	return recovered, nil
}

func (repository *SQLiteRepository) recoverOne(ctx context.Context, taskID string, now time.Time, policy RecoveryPolicy) (bool, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("开始任务恢复事务: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return false, err
	}
	if current.State != StateInterrupted && (current.LeaseExpiresAt == nil || current.LeaseExpiresAt.After(now)) {
		return false, nil
	}
	var next Task
	switch current.State {
	case StateLeased:
		next, err = Apply(current, Command{Kind: CommandLeaseExpired, Now: now})
	case StateRunning:
		next, err = Apply(current, Command{
			Kind: CommandInterrupt, Now: now, LeaseOwner: current.LeaseOwner, ErrorCode: repository.recoveryErrorCode,
		})
		if err == nil && next.State == StateInterrupted {
			next, err = Apply(next, Command{Kind: CommandRecover, Now: now, Resumable: policy(current)})
		}
	case StateInterrupted:
		next, err = Apply(current, Command{Kind: CommandRecover, Now: now, Resumable: policy(current)})
	default:
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return false, err
	}
	if current.State == StateRunning {
		outcome := string(StateInterrupted)
		if next.State == StateCancelled {
			outcome = string(StateCancelled)
		}
		result, err := transaction.ExecContext(ctx, `UPDATE mzfw_task_attempts
SET finished_at = ?, outcome = ?, error_code = ?, error_summary = ?
WHERE task_id = ? AND attempt_no = ? AND finished_at IS NULL`,
			formatTime(now), outcome, repository.recoveryErrorCode, repository.recoveryErrorSummary, taskID, current.AttemptCount)
		if err != nil {
			return false, fmt.Errorf("结束中断 attempt: %w", err)
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return false, fmt.Errorf("%w: 中断 attempt 不存在", ErrConflict)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("提交任务恢复: %w", err)
	}
	return true, nil
}

func collectTaskIDs(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("扫描 task id: %w", err)
		}
		ids = append(ids, taskID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 task id: %w", err)
	}
	return ids, nil
}

func (repository *SQLiteRepository) transition(ctx context.Context, taskID string, command Command) (Task, error) {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, fmt.Errorf("开始任务转换事务: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return Task{}, err
	}
	next, err := Apply(current, command)
	if err != nil {
		return Task{}, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Task{}, fmt.Errorf("提交任务转换: %w", err)
	}
	return next, nil
}

func (repository *SQLiteRepository) idempotentTransition(
	ctx context.Context,
	operation, taskID, reason, idempotencyKey string,
	command Command,
) (Task, bool, error) {
	if taskID == "" || idempotencyKey == "" || len(idempotencyKey) > 128 || len(reason) > 512 ||
		strings.ContainsRune(reason, '\x00') || command.Now.IsZero() {
		return Task{}, false, fmt.Errorf("%w: 幂等任务操作参数无效", ErrInvalidArgument)
	}
	fingerprint := operationFingerprint(operation, taskID, reason)
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, fmt.Errorf("开始幂等任务操作: %w", err)
	}
	defer transaction.Rollback()
	var storedOperation, storedFingerprint, storedTaskID string
	err = transaction.QueryRowContext(ctx, `SELECT operation, request_fingerprint, task_id
FROM mzfw_task_idempotency WHERE idempotency_key = ?`, idempotencyKey).Scan(&storedOperation, &storedFingerprint, &storedTaskID)
	if err == nil {
		if storedOperation != operation || storedFingerprint != fingerprint || storedTaskID != taskID {
			return Task{}, false, fmt.Errorf("%w: idempotency key 已对应其他操作", ErrConflict)
		}
		task, getErr := getTaskByID(ctx, transaction, taskID)
		if getErr != nil {
			return Task{}, false, getErr
		}
		return task, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, fmt.Errorf("读取幂等任务操作: %w", err)
	}
	current, err := getTaskByID(ctx, transaction, taskID)
	if err != nil {
		return Task{}, false, err
	}
	next, err := Apply(current, command)
	if err != nil {
		return Task{}, false, err
	}
	if err := updateTask(ctx, transaction, current, next); err != nil {
		return Task{}, false, err
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO mzfw_task_idempotency (
idempotency_key, operation, request_fingerprint, task_id, result_revision, created_at
) VALUES (?, ?, ?, ?, ?, ?)`, idempotencyKey, operation, fingerprint, taskID, next.Revision, formatTime(command.Now))
	if err != nil {
		return Task{}, false, fmt.Errorf("记录幂等任务操作: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return Task{}, false, fmt.Errorf("提交幂等任务操作: %w", err)
	}
	return next, true, nil
}

func operationFingerprint(operation, taskID, reason string) string {
	digest := sha256.Sum256([]byte(operation + "\x00" + taskID + "\x00" + reason))
	return hex.EncodeToString(digest[:])
}

type normalizedCreateRequest struct {
	CreateRequest
	capabilitiesJSON string
}

type normalizedCheckpointRequest struct {
	CheckpointRequest
}

func normalizeCheckpointRequest(request CheckpointRequest) (normalizedCheckpointRequest, error) {
	if request.TaskID == "" || !identifierPattern.MatchString(request.TaskID) || len(request.TaskID) > 128 ||
		request.AttemptNo < 1 || request.AttemptNo > MaxAttempts || !identifierPattern.MatchString(request.Key) ||
		len(request.Key) > 128 || request.Now.IsZero() || len(request.Payload) == 0 ||
		len(request.Payload) > MaxCheckpointBytes || !json.Valid(request.Payload) {
		return normalizedCheckpointRequest{}, fmt.Errorf("%w: checkpoint 字段无效", ErrInvalidArgument)
	}
	var value any
	if err := json.Unmarshal(request.Payload, &value); err != nil {
		return normalizedCheckpointRequest{}, fmt.Errorf("%w: checkpoint payload 无效", ErrInvalidArgument)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return normalizedCheckpointRequest{}, fmt.Errorf("%w: checkpoint payload 无法序列化", ErrInvalidArgument)
	}
	request.Payload = canonical
	request.Now = request.Now.UTC()
	return normalizedCheckpointRequest{CheckpointRequest: request}, nil
}

func normalizeCreateRequest(request CreateRequest) (normalizedCreateRequest, error) {
	if !identifierPattern.MatchString(request.Type) || len(request.Type) > 128 ||
		request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 ||
		request.PayloadVersion < 1 || request.MaxAttempts < 1 || request.MaxAttempts > MaxAttempts ||
		request.Priority < -100 || request.Priority > 100 || request.Now.IsZero() {
		return normalizedCreateRequest{}, fmt.Errorf("%w: 创建任务字段无效", ErrInvalidArgument)
	}
	if len(request.Payload) == 0 || len(request.Payload) > MaxPayloadBytes || !json.Valid(request.Payload) {
		return normalizedCreateRequest{}, fmt.Errorf("%w: payload 无效", ErrInvalidArgument)
	}
	var value any
	if err := json.Unmarshal(request.Payload, &value); err != nil {
		return normalizedCreateRequest{}, fmt.Errorf("%w: payload JSON: %v", ErrInvalidArgument, err)
	}
	canonicalPayload, err := json.Marshal(value)
	if err != nil {
		return normalizedCreateRequest{}, fmt.Errorf("%w: payload JSON: %v", ErrInvalidArgument, err)
	}
	capabilities, err := normalizeCapabilities(request.RequiredCapabilities)
	if err != nil {
		return normalizedCreateRequest{}, err
	}
	capabilitiesJSON, _ := json.Marshal(capabilities)
	request.Payload = canonicalPayload
	request.RequiredCapabilities = capabilities
	request.Now = request.Now.UTC()
	if request.ScheduledAt != nil {
		value := request.ScheduledAt.UTC()
		request.ScheduledAt = &value
	}
	return normalizedCreateRequest{CreateRequest: request, capabilitiesJSON: string(capabilitiesJSON)}, nil
}

func normalizeCapabilities(values []string) ([]string, error) {
	if len(values) > 64 {
		return nil, fmt.Errorf("%w: capability 数量超过上限", ErrInvalidArgument)
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !identifierPattern.MatchString(value) || len(value) > 128 {
			return nil, fmt.Errorf("%w: capability 无效", ErrInvalidArgument)
		}
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeListRequest(request ListRequest) ([]State, []string, error) {
	if request.Limit < 1 || request.Limit > MaxTaskPage || len(request.States) > 16 || len(request.Types) > 64 ||
		len(request.WorkspaceID) > 256 || strings.ContainsRune(request.WorkspaceID, '\x00') ||
		(request.BeforeCreatedAt == nil) != (request.BeforeTaskID == "") {
		return nil, nil, fmt.Errorf("%w: 任务分页参数无效", ErrInvalidArgument)
	}
	if request.BeforeCreatedAt != nil && (!identifierPattern.MatchString(request.BeforeTaskID) || len(request.BeforeTaskID) > 128) {
		return nil, nil, fmt.Errorf("%w: 任务分页 cursor 无效", ErrInvalidArgument)
	}
	stateSet := make(map[State]struct{}, len(request.States))
	for _, state := range request.States {
		if !validState(state) {
			return nil, nil, fmt.Errorf("%w: 任务状态过滤无效", ErrInvalidArgument)
		}
		stateSet[state] = struct{}{}
	}
	states := make([]State, 0, len(stateSet))
	for state := range stateSet {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i] < states[j] })
	typeSet := make(map[string]struct{}, len(request.Types))
	for _, taskType := range request.Types {
		if !identifierPattern.MatchString(taskType) || len(taskType) > 128 {
			return nil, nil, fmt.Errorf("%w: 任务类型过滤无效", ErrInvalidArgument)
		}
		typeSet[taskType] = struct{}{}
	}
	types := make([]string, 0, len(typeSet))
	for taskType := range typeSet {
		types = append(types, taskType)
	}
	sort.Strings(types)
	return states, types, nil
}

func validState(state State) bool {
	switch state {
	case StatePending, StateLeased, StateRunning, StateRetryWait, StateSucceeded,
		StateFailed, StateCancelled, StateInterrupted, StateManualReview:
		return true
	default:
		return false
	}
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func sameCreateSemantics(task Task, request normalizedCreateRequest) bool {
	return task.Type == request.Type && task.Priority == request.Priority &&
		task.PayloadVersion == request.PayloadVersion && task.PayloadJSON == string(request.Payload) &&
		task.WorkspaceID == request.WorkspaceID && task.RequiredCapabilitiesJSON == request.capabilitiesJSON &&
		task.MaxAttempts == request.MaxAttempts && equalOptionalTime(task.ScheduledAt, request.ScheduledAt)
}

func equalOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

const taskColumns = `task_id, task_type, state, priority, payload_version, payload_json,
idempotency_key, workspace_id, required_capabilities_json, created_at, scheduled_at,
started_at, finished_at, lease_owner, lease_expires_at, attempt_count, max_attempts,
next_retry_at, cancel_requested_at, result_summary_json, error_code, revision`

type rowScanner interface {
	Scan(...any) error
}

type taskQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getTaskByID(ctx context.Context, query taskQuerier, taskID string) (Task, error) {
	task, err := scanTask(query.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM mzfw_tasks WHERE task_id = ?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, taskID)
	}
	return task, err
}

func getTaskByIdempotencyKey(ctx context.Context, query taskQuerier, key string) (Task, error) {
	task, err := scanTask(query.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM mzfw_tasks WHERE idempotency_key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

func getCheckpointByKey(ctx context.Context, query taskQuerier, taskID, key string) (Checkpoint, bool, error) {
	checkpoint, err := scanCheckpoint(query.QueryRowContext(ctx, `SELECT
task_id, checkpoint_key, checkpoint_revision, attempt_no, output_cursor, payload_json, created_at
FROM mzfw_task_checkpoints WHERE task_id = ? AND checkpoint_key = ?`, taskID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, false, nil
	}
	if err != nil {
		return Checkpoint{}, false, fmt.Errorf("读取 task checkpoint: %w", err)
	}
	return checkpoint, true, nil
}

func scanCheckpoint(row rowScanner) (Checkpoint, error) {
	var checkpoint Checkpoint
	var createdAt string
	if err := row.Scan(&checkpoint.TaskID, &checkpoint.Key, &checkpoint.Revision, &checkpoint.AttemptNo,
		&checkpoint.OutputCursor, &checkpoint.PayloadJSON, &createdAt); err != nil {
		return Checkpoint{}, err
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return Checkpoint{}, err
	}
	checkpoint.CreatedAt = created
	return checkpoint, nil
}

func scanTask(row rowScanner) (Task, error) {
	var task Task
	var state string
	var createdAt string
	var scheduledAt, startedAt, finishedAt sql.NullString
	var leaseOwner, leaseExpiresAt, nextRetryAt, cancelRequestedAt sql.NullString
	var workspaceID, resultSummary, errorCode sql.NullString
	err := row.Scan(
		&task.ID, &task.Type, &state, &task.Priority, &task.PayloadVersion, &task.PayloadJSON,
		&task.IdempotencyKey, &workspaceID, &task.RequiredCapabilitiesJSON, &createdAt, &scheduledAt,
		&startedAt, &finishedAt, &leaseOwner, &leaseExpiresAt, &task.AttemptCount, &task.MaxAttempts,
		&nextRetryAt, &cancelRequestedAt, &resultSummary, &errorCode, &task.Revision,
	)
	if err != nil {
		return Task{}, err
	}
	task.State = State(state)
	task.WorkspaceID = workspaceID.String
	task.LeaseOwner = leaseOwner.String
	task.ResultSummaryJSON = resultSummary.String
	task.ErrorCode = errorCode.String
	if task.CreatedAt, err = parseTime(createdAt); err != nil {
		return Task{}, err
	}
	for _, item := range []struct {
		source sql.NullString
		target **time.Time
	}{
		{scheduledAt, &task.ScheduledAt}, {startedAt, &task.StartedAt}, {finishedAt, &task.FinishedAt},
		{leaseExpiresAt, &task.LeaseExpiresAt}, {nextRetryAt, &task.NextRetryAt}, {cancelRequestedAt, &task.CancelRequestedAt},
	} {
		if item.source.Valid {
			parsed, parseErr := parseTime(item.source.String)
			if parseErr != nil {
				return Task{}, parseErr
			}
			*item.target = &parsed
		}
	}
	return task, nil
}

func updateTask(ctx context.Context, transaction *sql.Tx, current, next Task) error {
	result, err := transaction.ExecContext(ctx, `UPDATE mzfw_tasks SET
	state = ?, started_at = ?, finished_at = ?, lease_owner = ?, lease_expires_at = ?,
	attempt_count = ?, max_attempts = ?, next_retry_at = ?, cancel_requested_at = ?, result_summary_json = ?,
	error_code = ?, revision = ? WHERE task_id = ? AND revision = ?`,
		next.State, nullableTime(next.StartedAt), nullableTime(next.FinishedAt), nullableString(next.LeaseOwner),
		nullableTime(next.LeaseExpiresAt), next.AttemptCount, next.MaxAttempts, nullableTime(next.NextRetryAt),
		nullableTime(next.CancelRequestedAt), nullableString(next.ResultSummaryJSON), nullableString(next.ErrorCode),
		next.Revision, next.ID, current.Revision)
	if err != nil {
		return fmt.Errorf("更新任务状态: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取任务更新结果: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: task revision 已变化", ErrConflict)
	}
	return nil
}

func nextOutputCursor(ctx context.Context, query taskQuerier, taskID string) (int64, error) {
	var cursor int64
	if err := query.QueryRowContext(ctx, `SELECT COALESCE(MAX(cursor), 0) + 1 FROM mzfw_task_output WHERE task_id = ?`, taskID).Scan(&cursor); err != nil {
		return 0, fmt.Errorf("分配任务输出 cursor: %w", err)
	}
	return cursor, nil
}

func currentOutputCursor(ctx context.Context, query taskQuerier, taskID string) (int64, error) {
	var cursor int64
	if err := query.QueryRowContext(ctx, `SELECT COALESCE(MAX(cursor), 0) FROM mzfw_task_output WHERE task_id = ?`, taskID).Scan(&cursor); err != nil {
		return 0, fmt.Errorf("读取任务输出 cursor: %w", err)
	}
	return cursor, nil
}

const timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatTime(value time.Time) string {
	return value.UTC().Format(timestampLayout)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析任务时间: %w", err)
	}
	return parsed, nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func randomID(prefix string) string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}
