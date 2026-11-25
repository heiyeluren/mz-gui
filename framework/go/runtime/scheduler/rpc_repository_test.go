package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
)

func TestRPCRepositoryCreateAndUpdateArePersistentlyIdempotent(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "rpc-create")
	created, applied, err := repository.CreateIdempotent(ctx, request, "schedule-create-1")
	if err != nil || !applied || created.Revision != 1 {
		t.Fatalf("幂等创建失败: value=%#v applied=%v err=%v", created, applied, err)
	}
	replayed, applied, err := repository.CreateIdempotent(ctx, request, "schedule-create-1")
	if err != nil || applied || replayed.ID != created.ID {
		t.Fatalf("幂等创建重放失败: value=%#v applied=%v err=%v", replayed, applied, err)
	}
	different := request
	different.Name = "different"
	if _, _, err := repository.CreateIdempotent(ctx, different, "schedule-create-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键不同创建未冲突: %v", err)
	}

	update := validScheduleRequest(now.Add(time.Second), "rpc-updated")
	updated, applied, err := repository.UpdateIdempotent(ctx, UpdateRequest{
		ID: created.ID, ExpectedRevision: created.Revision, CreateRequest: update,
	}, "schedule-update-1")
	if err != nil || !applied || updated.Revision != 2 || updated.Name != "rpc-updated" {
		t.Fatalf("幂等更新失败: value=%#v applied=%v err=%v", updated, applied, err)
	}
	replayed, applied, err = repository.UpdateIdempotent(ctx, UpdateRequest{
		ID: created.ID, ExpectedRevision: created.Revision, CreateRequest: update,
	}, "schedule-update-1")
	if err != nil || applied || replayed.ID != created.ID || replayed.Revision != 2 {
		t.Fatalf("幂等更新重放失败: value=%#v applied=%v err=%v", replayed, applied, err)
	}
}

func TestRPCRepositoryEnableDeleteAndKeyConflicts(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "rpc-state")
	request.Enabled = false
	value, _, err := repository.CreateIdempotent(ctx, request, "state-create")
	if err != nil {
		t.Fatal(err)
	}
	enabled, applied, err := repository.SetEnabledIdempotent(ctx, value.ID, 1, true, "state-enable", now.Add(time.Second))
	if err != nil || !applied || !enabled.Enabled || enabled.Revision != 2 {
		t.Fatalf("幂等启用失败: value=%#v applied=%v err=%v", enabled, applied, err)
	}
	replayed, applied, err := repository.SetEnabledIdempotent(ctx, value.ID, 1, true, "state-enable", now.Add(2*time.Second))
	if err != nil || applied || replayed.Revision != 2 {
		t.Fatalf("幂等启用重放失败: value=%#v applied=%v err=%v", replayed, applied, err)
	}
	if _, _, err := repository.SetEnabledIdempotent(ctx, value.ID, 2, false, "state-enable", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("跨操作重用键未冲突: %v", err)
	}
	deleted, applied, err := repository.DeleteIdempotent(ctx, value.ID, 2, "state-delete", now.Add(3*time.Second))
	if err != nil || !applied || deleted.Revision != 3 {
		t.Fatalf("幂等删除失败: value=%#v applied=%v err=%v", deleted, applied, err)
	}
	repeated, applied, err := repository.DeleteIdempotent(ctx, value.ID, 2, "state-delete", now.Add(4*time.Second))
	if err != nil || applied || repeated != deleted {
		t.Fatalf("幂等删除重放失败: value=%#v applied=%v err=%v", repeated, applied, err)
	}
}

func TestRPCRepositoryRunNowCreatesOneOccurrenceAndTask(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	schedule, _, err := repository.CreateIdempotent(ctx, validScheduleRequest(now, "run-now"), "run-create")
	if err != nil {
		t.Fatal(err)
	}
	runAt := now.Add(time.Second)
	result, applied, err := repository.RunNowIdempotent(ctx, schedule.ID, 1, "run-now-1", runAt)
	if err != nil || !applied || result.Schedule.Revision != 2 || result.Occurrence.TaskID == "" {
		t.Fatalf("立即运行失败: result=%#v applied=%v err=%v", result, applied, err)
	}
	replayed, applied, err := repository.RunNowIdempotent(ctx, schedule.ID, 1, "run-now-1", runAt.Add(time.Second))
	if err != nil || applied || replayed.Occurrence.ID != result.Occurrence.ID {
		t.Fatalf("立即运行重放失败: result=%#v applied=%v err=%v", replayed, applied, err)
	}
	page, err := tasks.List(ctx, taskengine.ListRequest{Limit: 10})
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("立即运行任务数错误: page=%#v err=%v", page, err)
	}
	occurrences, err := repository.ListOccurrences(ctx, OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 10})
	if err != nil || len(occurrences.Occurrences) != 1 || occurrences.Occurrences[0].ID != result.Occurrence.ID {
		t.Fatalf("立即运行 occurrence 错误: page=%#v err=%v", occurrences, err)
	}
}

func TestRPCRepositoryOccurrenceCursorAndValidation(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	schedule, _, err := repository.CreateIdempotent(ctx, validScheduleRequest(now, "occurrence-page"), "page-create")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := repository.RunNowIdempotent(ctx, schedule.ID, 1, "page-run-1", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := repository.RunNowIdempotent(ctx, schedule.ID, 2, "page-run-2", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListOccurrences(ctx, OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 1})
	if err != nil || !page.HasMore || len(page.Occurrences) != 1 || page.Occurrences[0].ID != second.Occurrence.ID {
		t.Fatalf("occurrence 首屏错误: page=%#v err=%v", page, err)
	}
	next, err := repository.ListOccurrences(ctx, OccurrenceListRequest{
		ScheduleID: schedule.ID, BeforePlannedAt: page.NextPlannedAt, BeforeOccurrenceID: page.NextOccurrenceID, Limit: 1,
	})
	if err != nil || next.HasMore || len(next.Occurrences) != 1 || next.Occurrences[0].ID != first.Occurrence.ID {
		t.Fatalf("occurrence 续页错误: page=%#v err=%v", next, err)
	}
	for _, request := range []OccurrenceListRequest{
		{},
		{ScheduleID: schedule.ID},
		{ScheduleID: schedule.ID, Limit: 1, BeforePlannedAt: &now},
	} {
		if _, err := repository.ListOccurrences(ctx, request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法 occurrence 列表未拒绝: request=%#v err=%v", request, err)
		}
	}
	if _, _, err := repository.CreateIdempotent(ctx, validScheduleRequest(now, "bad-key"), "bad key"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法幂等键未拒绝: %v", err)
	}
}

func TestRPCRepositoryRejectsMutationConflictsAndExpiredOnce(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "mutation-conflict")
	schedule, _, err := repository.CreateIdempotent(ctx, request, "conflict-create")
	if err != nil {
		t.Fatal(err)
	}

	badUpdate := UpdateRequest{ID: schedule.ID, ExpectedRevision: 2, CreateRequest: request}
	if _, _, err := repository.UpdateIdempotent(ctx, badUpdate, "conflict-update"); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 更新错误 = %v", err)
	}
	if _, _, err := repository.SetEnabledIdempotent(ctx, schedule.ID, 2, false, "conflict-enable", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 启停错误 = %v", err)
	}
	if _, _, err := repository.DeleteIdempotent(ctx, schedule.ID, 2, "conflict-delete", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 删除错误 = %v", err)
	}
	if _, _, err := repository.RunNowIdempotent(ctx, schedule.ID, 2, "conflict-run", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 立即运行错误 = %v", err)
	}
	if _, _, err := repository.DeleteIdempotent(ctx, "missing", 1, "missing-delete", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在调度错误 = %v", err)
	}
	if _, _, err := repository.RunNowIdempotent(ctx, "missing", 1, "missing-run", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("运行不存在调度错误 = %v", err)
	}

	once := validScheduleRequest(now, "expired-once")
	once.Enabled = false
	once.Kind = KindOnce
	once.Expression = now.Add(time.Second).Format(time.RFC3339Nano)
	expired, _, err := repository.CreateIdempotent(ctx, once, "expired-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.SetEnabledIdempotent(ctx, expired.ID, 1, true, "expired-enable", now.Add(2*time.Second)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("过期 once 启用错误 = %v", err)
	}
}

func TestRPCRepositoryCoversInvalidAndClosedDatabasePaths(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	now := time.Now().UTC()
	valid := validScheduleRequest(now, "closed")
	invalidCalls := []func() error{
		func() error {
			_, _, err := repository.UpdateIdempotent(context.Background(), UpdateRequest{}, "key")
			return err
		},
		func() error {
			_, _, err := repository.SetEnabledIdempotent(context.Background(), "", 0, false, "key", now)
			return err
		},
		func() error {
			_, _, err := repository.DeleteIdempotent(context.Background(), "", 0, "key", now)
			return err
		},
		func() error {
			_, _, err := repository.RunNowIdempotent(context.Background(), "", 0, "key", now)
			return err
		},
	}
	for _, call := range invalidCalls {
		if err := call(); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法幂等参数错误 = %v", err)
		}
	}
	closeDatabase()
	closedCalls := []func() error{
		func() error {
			_, _, err := repository.CreateIdempotent(context.Background(), valid, "closed-create")
			return err
		},
		func() error {
			_, _, err := repository.UpdateIdempotent(context.Background(), UpdateRequest{ID: "schedule", ExpectedRevision: 1, CreateRequest: valid}, "closed-update")
			return err
		},
		func() error {
			_, _, err := repository.SetEnabledIdempotent(context.Background(), "schedule", 1, false, "closed-enable", now)
			return err
		},
		func() error {
			_, _, err := repository.DeleteIdempotent(context.Background(), "schedule", 1, "closed-delete", now)
			return err
		},
		func() error {
			_, _, err := repository.RunNowIdempotent(context.Background(), "schedule", 1, "closed-run", now)
			return err
		},
	}
	for _, call := range closedCalls {
		if err := call(); err == nil || errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("关闭数据库错误未传播: %v", err)
		}
	}
}

func TestRPCRepositoryRejectsMissingAndCorruptOccurrences(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	schedule, _, err := repository.CreateIdempotent(ctx, validScheduleRequest(now, "corrupt-occurrence"), "corrupt-create")
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getOccurrence(ctx, transaction, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空 occurrence 错误 = %v", err)
	}
	if _, err := getOccurrence(ctx, transaction, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失 occurrence 错误 = %v", err)
	}
	transaction.Rollback()
	_, err = repository.database.ExecContext(ctx, `INSERT INTO mzfw_schedule_occurrences (
occurrence_id, schedule_id, planned_at, task_id, state, created_at, reason_code
) VALUES (?, ?, ?, NULL, ?, ?, NULL)`, "corrupt-occurrence", schedule.ID, "bad-time", "created", formatScheduleTime(now))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListOccurrences(ctx, OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 10}); err == nil {
		t.Fatal("损坏 occurrence 时间必须拒绝")
	}
	if _, err := repository.ListOccurrences(ctx, OccurrenceListRequest{ScheduleID: "missing", Limit: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在调度的 occurrence 列表错误 = %v", err)
	}
}

func TestRPCRepositoryRollsBackOnGeneratedIDAndDatabaseFailures(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "failure-path")

	emptyIDs := NewSQLiteRepository(repository.database, repository.tasks, func(string) string { return "" })
	if _, _, err := emptyIDs.CreateIdempotent(ctx, request, "empty-id-create"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空 schedule id 错误 = %v", err)
	}
	fixedIDs := NewSQLiteRepository(repository.database, repository.tasks, func(string) string { return "schedule-fixed" })
	if _, _, err := fixedIDs.CreateIdempotent(ctx, request, "fixed-create-1"); err != nil {
		t.Fatal(err)
	}
	request.Name = "duplicate-id"
	if _, _, err := fixedIDs.CreateIdempotent(ctx, request, "fixed-create-2"); err == nil {
		t.Fatal("重复生成的 schedule id 必须失败")
	}

	if _, err := repository.database.ExecContext(ctx, `CREATE TRIGGER fail_schedule_ledger
BEFORE INSERT ON mzfw_schedule_idempotency BEGIN SELECT RAISE(ABORT, 'ledger failure'); END`); err != nil {
		t.Fatal(err)
	}
	request.Name = "ledger-rollback"
	if _, _, err := repository.CreateIdempotent(ctx, request, "ledger-create"); err == nil {
		t.Fatal("幂等账本写入失败必须回滚")
	}
	var count int
	if err := repository.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mzfw_schedules WHERE name = ?`, request.Name).Scan(&count); err != nil || count != 0 {
		t.Fatalf("账本失败留下半状态: count=%d err=%v", count, err)
	}
}

func TestRPCRepositoryRollsBackUpdateWhenLedgerWriteFails(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	created, _, err := repository.CreateIdempotent(ctx, validScheduleRequest(now, "rollback-before"), "rollback-update-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.ExecContext(ctx, `CREATE TRIGGER fail_schedule_update_ledger
BEFORE INSERT ON mzfw_schedule_idempotency BEGIN SELECT RAISE(ABORT, 'update ledger failure'); END`); err != nil {
		t.Fatal(err)
	}

	request := validScheduleRequest(now.Add(time.Second), "rollback-after")
	if _, _, err := repository.UpdateIdempotent(ctx, UpdateRequest{
		ID: created.ID, ExpectedRevision: created.Revision, CreateRequest: request,
	}, "rollback-update-ledger"); err == nil {
		t.Fatal("更新后账本写入失败必须让整个事务失败")
	}
	current, err := repository.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Name != created.Name || current.Revision != created.Revision {
		t.Fatalf("更新账本失败留下半状态: current=%#v created=%#v", current, created)
	}
}
