package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	schedulermigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/scheduler/migrations"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestSQLiteRepositoryDefaultIDAndInputBoundaries(t *testing.T) {
	database, closeDatabase := newSchedulerDatabase(t, "framework-scheduler-default-")
	defer closeDatabase()
	tasks := taskengine.NewSQLiteRepository(database, nil)
	repository := NewSQLiteRepository(database, tasks, nil)
	now := time.Now().UTC()
	value, err := repository.Create(context.Background(), validScheduleRequest(now, "default-id"))
	if err != nil || len(value.ID) < 20 {
		t.Fatalf("默认 ID 错误: value=%#v err=%v", value, err)
	}
	if _, err := repository.Get(context.Background(), ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空 ID 错误 = %v", err)
	}
	for _, request := range []ListRequest{{Limit: 0}, {Limit: MaxSchedulePage + 1}} {
		if _, err := repository.List(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法列表错误 = %v", err)
		}
	}
	if _, err := repository.Due(context.Background(), time.Time{}, 1); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 due 错误 = %v", err)
	}
	if _, err := repository.SetEnabled(context.Background(), "", 0, true, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法启停错误 = %v", err)
	}
	if err := repository.Delete(context.Background(), "", 0, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法删除错误 = %v", err)
	}
}

func TestSQLiteRepositoryScheduleCRUDUsesRevisionAndSoftDelete(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	schedule, err := repository.Create(ctx, validScheduleRequest(now, "schedule-a"))
	if err != nil || schedule.ID == "" || schedule.Revision != 1 || schedule.NextRunAt == nil || !schedule.NextRunAt.After(now) {
		t.Fatalf("创建 schedule: value=%#v err=%v", schedule, err)
	}
	loaded, err := repository.Get(ctx, schedule.ID)
	if err != nil || loaded.ID != schedule.ID || loaded.TaskTemplateJSON != `{"task_type":"test.echo","payload_version":1,"payload":{},"priority":0,"max_attempts":1}` {
		t.Fatalf("读取 schedule: value=%#v err=%v", loaded, err)
	}
	update := validScheduleRequest(now.Add(time.Second), "schedule-a-updated")
	updated, err := repository.Update(ctx, UpdateRequest{ID: schedule.ID, ExpectedRevision: 1, CreateRequest: update})
	if err != nil || updated.Name != "schedule-a-updated" || updated.Revision != 2 {
		t.Fatalf("更新 schedule: value=%#v err=%v", updated, err)
	}
	if _, err := repository.Update(ctx, UpdateRequest{ID: schedule.ID, ExpectedRevision: 1, CreateRequest: update}); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 更新错误 = %v", err)
	}
	disabled, err := repository.SetEnabled(ctx, schedule.ID, 2, false, now.Add(2*time.Second))
	if err != nil || disabled.Enabled || disabled.Revision != 3 || disabled.NextRunAt != nil {
		t.Fatalf("禁用 schedule: value=%#v err=%v", disabled, err)
	}
	if err := repository.Delete(ctx, schedule.ID, 3, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(ctx, schedule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("软删除后读取错误 = %v", err)
	}
}

func TestSQLiteRepositoryValidatesScheduleAndTemplate(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	tests := []CreateRequest{
		{},
		func() CreateRequest {
			value := validScheduleRequest(now, "bad-timezone")
			value.Timezone = "GMT+8"
			return value
		}(),
		func() CreateRequest {
			value := validScheduleRequest(now, "bad-template")
			value.TaskTemplate = json.RawMessage(`{"unknown":true}`)
			return value
		}(),
		func() CreateRequest {
			value := validScheduleRequest(now, "bad-overlap")
			value.OverlapLimit = 0
			return value
		}(),
		func() CreateRequest {
			value := validScheduleRequest(now, "bad-jitter")
			value.Jitter = 2 * time.Hour
			return value
		}(),
	}
	for _, request := range tests {
		if _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法 schedule 未拒绝: request=%#v err=%v", request, err)
		}
	}
}

func TestSQLiteRepositoryAppliesOccurrenceAndTaskAtomically(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "atomic")
	request.Jitter = 10 * time.Second
	schedule, err := repository.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	planned := *schedule.NextRunAt
	plan := Plan{
		Occurrences: []PlannedOccurrence{{PlannedAt: planned, ScheduledAt: planned.Add(3 * time.Second)}},
		NextRunAt:   timeRef(planned.Add(15 * time.Minute)),
	}
	occurrences, err := repository.ApplyPlan(ctx, schedule, plan, now.Add(time.Second))
	if err != nil || len(occurrences) != 1 || occurrences[0].TaskID == "" || occurrences[0].State != "created" {
		t.Fatalf("应用 occurrence: values=%#v err=%v", occurrences, err)
	}
	task, err := tasks.Get(ctx, occurrences[0].TaskID)
	if err != nil || task.Type != "test.echo" || task.ScheduledAt == nil || !task.ScheduledAt.Equal(planned.Add(3*time.Second)) {
		t.Fatalf("原子任务错误: task=%#v err=%v", task, err)
	}
	advanced, err := repository.Get(ctx, schedule.ID)
	if err != nil || advanced.Revision != 2 || advanced.LastRunAt == nil || !advanced.LastRunAt.Equal(planned) {
		t.Fatalf("schedule 未推进: value=%#v err=%v", advanced, err)
	}
	if _, err := repository.ApplyPlan(ctx, schedule, plan, now.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧计划重复提交错误 = %v", err)
	}
}

func TestSQLiteRepositoryEnforcesForbidAndReplaceOverlap(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, policy := range []OverlapPolicy{OverlapForbid, OverlapReplace} {
		request := validScheduleRequest(now, "overlap-"+string(policy))
		request.OverlapPolicy = policy
		schedule, err := repository.Create(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		firstAt := *schedule.NextRunAt
		secondAt := firstAt.Add(15 * time.Minute)
		plan := Plan{
			Occurrences: []PlannedOccurrence{{PlannedAt: firstAt, ScheduledAt: firstAt}, {PlannedAt: secondAt, ScheduledAt: secondAt}},
			NextRunAt:   timeRef(secondAt.Add(15 * time.Minute)),
		}
		occurrences, err := repository.ApplyPlan(ctx, schedule, plan, now.Add(time.Second))
		if err != nil || len(occurrences) != 2 {
			t.Fatalf("策略 %s 应用失败: occurrences=%#v err=%v", policy, occurrences, err)
		}
		firstTask, err := tasks.Get(ctx, occurrences[0].TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if policy == OverlapForbid {
			if occurrences[1].State != "skipped" || occurrences[1].ReasonCode != "OVERLAP_FORBIDDEN" || occurrences[1].TaskID != "" || firstTask.State != taskengine.StatePending {
				t.Fatalf("forbid 结果错误: occurrences=%#v task=%#v", occurrences, firstTask)
			}
		} else if occurrences[1].TaskID == "" || firstTask.State != taskengine.StateCancelled {
			t.Fatalf("replace 结果错误: occurrences=%#v first=%#v", occurrences, firstTask)
		}
	}
}

func TestSQLiteRepositoryDueAndListRespectEnabledState(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	first, _ := repository.Create(ctx, validScheduleRequest(now, "due-a"))
	request := validScheduleRequest(now, "due-b")
	request.Enabled = false
	_, _ = repository.Create(ctx, request)
	dueAt := first.NextRunAt.Add(time.Second)
	due, err := repository.Due(ctx, dueAt, 10)
	if err != nil || len(due) != 1 || due[0].ID != first.ID {
		t.Fatalf("due 查询错误: values=%#v err=%v", due, err)
	}
	active, err := repository.List(ctx, ListRequest{Limit: 10})
	if err != nil || len(active) != 1 {
		t.Fatalf("活动列表错误: values=%#v err=%v", active, err)
	}
	all, err := repository.List(ctx, ListRequest{IncludeDisabled: true, Limit: 10})
	if err != nil || len(all) != 2 {
		t.Fatalf("全列表错误: values=%#v err=%v", all, err)
	}
}

func TestSQLiteRepositoryReenablesScheduleAndHandlesConflicts(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "reenable")
	request.Enabled = false
	schedule, err := repository.Create(ctx, request)
	if err != nil || schedule.NextRunAt != nil {
		t.Fatalf("创建禁用 schedule: value=%#v err=%v", schedule, err)
	}
	enabled, err := repository.SetEnabled(ctx, schedule.ID, 1, true, now.Add(time.Second))
	if err != nil || !enabled.Enabled || enabled.NextRunAt == nil || enabled.Revision != 2 {
		t.Fatalf("重新启用失败: value=%#v err=%v", enabled, err)
	}
	if _, err := repository.SetEnabled(ctx, schedule.ID, 1, false, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 revision 启停错误 = %v", err)
	}
	if err := repository.Delete(ctx, "missing", 1, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除不存在 schedule 错误 = %v", err)
	}
	bad := validScheduleRequest(now, "bad-update")
	if _, err := repository.Update(ctx, UpdateRequest{CreateRequest: bad}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法更新错误 = %v", err)
	}
}

func TestSQLiteRepositoryAllowLimitedCapsCatchUpTasks(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := validScheduleRequest(now, "allow-limited")
	request.OverlapPolicy = OverlapAllowLimited
	request.OverlapLimit = 2
	schedule, err := repository.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	first := *schedule.NextRunAt
	plan := Plan{NextRunAt: timeRef(first.Add(time.Hour))}
	for index := 0; index < 3; index++ {
		planned := first.Add(time.Duration(index) * 15 * time.Minute)
		plan.Occurrences = append(plan.Occurrences, PlannedOccurrence{PlannedAt: planned, ScheduledAt: planned})
	}
	occurrences, err := repository.ApplyPlan(ctx, schedule, plan, now.Add(time.Second))
	if err != nil || len(occurrences) != 3 || occurrences[2].ReasonCode != "OVERLAP_LIMIT_REACHED" {
		t.Fatalf("allow_limited 错误: values=%#v err=%v", occurrences, err)
	}
	page, err := tasks.List(ctx, taskengine.ListRequest{Limit: 10})
	if err != nil || len(page.Tasks) != 2 {
		t.Fatalf("allow_limited 任务数错误: page=%#v err=%v", page, err)
	}
}

func TestSQLiteRepositoryRejectsMalformedPlanAndTrailingTemplateJSON(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	request := validScheduleRequest(now, "trailing-json")
	request.TaskTemplate = append(request.TaskTemplate, []byte(` {}`)...)
	if _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("多余 JSON 未拒绝: %v", err)
	}
	schedule, err := repository.Create(context.Background(), validScheduleRequest(now, "bad-plan"))
	if err != nil {
		t.Fatal(err)
	}
	badTime := schedule.NextRunAt.Add(-time.Second)
	if _, err := repository.ApplyPlan(context.Background(), schedule, Plan{
		Occurrences: []PlannedOccurrence{{PlannedAt: *schedule.NextRunAt, ScheduledAt: badTime}},
	}, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 occurrence 时间错误 = %v", err)
	}
}

func newSchedulerRepository(t *testing.T) (*SQLiteRepository, *taskengine.SQLiteRepository, func()) {
	t.Helper()
	database, closeDatabase := newSchedulerDatabase(t, "framework-scheduler-")
	ids := &schedulerSequenceIDs{}
	tasks := taskengine.NewSQLiteRepository(database, ids.Next)
	repository := NewSQLiteRepository(database, tasks, ids.Next)
	return repository, tasks, closeDatabase
}

func newSchedulerDatabase(t *testing.T, prefix string) (*sql.DB, func()) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatal(err)
	}
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(root, "scheduler.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			taskmigrations.Bundle(),
			schedulermigrations.Bundle(),
			{
				Owner: "application.scheduler-test", Dependencies: []string{schedulermigrations.Owner},
				Migrations: os.DirFS("testdata"),
			},
		},
	})
	if err != nil {
		os.RemoveAll(root)
		t.Fatal(err)
	}
	if err := taskmigrations.ReconcileLegacy(context.Background(), database); err != nil {
		database.Close()
		os.RemoveAll(root)
		t.Fatal(err)
	}
	if err := schedulermigrations.ReconcileLegacy(context.Background(), database); err != nil {
		database.Close()
		os.RemoveAll(root)
		t.Fatal(err)
	}
	return database, func() {
		database.Close()
		os.RemoveAll(root)
	}
}

func validScheduleRequest(now time.Time, name string) CreateRequest {
	return CreateRequest{
		Name: name, Enabled: true, Kind: KindInterval,
		Expression: now.Add(time.Minute).Format(time.RFC3339) + "/15m", Timezone: "UTC",
		MisfirePolicy: MisfireRunOnce, OverlapPolicy: OverlapForbid,
		MaxCatchUp: 1, OverlapLimit: 1, TaskTemplate: json.RawMessage(`{
            "task_type":"test.echo", "payload_version":1, "payload":{},
            "priority":0, "max_attempts":1
        }`), Now: now,
	}
}

func timeRef(value time.Time) *time.Time { return &value }

type schedulerSequenceIDs struct{ value int }

func (ids *schedulerSequenceIDs) Next(prefix string) string {
	ids.value++
	return fmt.Sprintf("%s-%d", prefix, ids.value)
}
