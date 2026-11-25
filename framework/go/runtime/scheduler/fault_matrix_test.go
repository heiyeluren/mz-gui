package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	schedulermigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/scheduler/migrations"
	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

// jumpClock 用墙钟跳跃模拟休眠/唤醒，不调用 time.Sleep，也不休眠本机。
type jumpClock struct {
	mu  sync.Mutex
	now time.Time
}

func newJumpClock(now time.Time) *jumpClock {
	return &jumpClock{now: now.UTC()}
}

func (clock *jumpClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *jumpClock) Jump(delta time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delta)
}

func TestEngineSleepAcrossTriggerFollowsMisfirePolicy(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		policy        MisfirePolicy
		overlap       OverlapPolicy
		overlapLimit  int
		maxCatchUp    int
		wantCreated   int
		wantSkipped   int
		wantSkipCode  string
		wantLastRun   bool
		wantLastEqual time.Time
	}{
		{
			policy: MisfireSkip, overlap: OverlapAllowLimited, overlapLimit: 16, maxCatchUp: 4,
		},
		{
			policy: MisfireRunOnce, overlap: OverlapAllowLimited, overlapLimit: 16, maxCatchUp: 4,
			wantCreated: 1, wantLastRun: true, wantLastEqual: now,
		},
		{
			policy: MisfireCatchUpLimited, overlap: OverlapAllowLimited, overlapLimit: 16, maxCatchUp: 3,
			wantCreated: 3, wantLastRun: true, wantLastEqual: now.Add(30 * time.Minute),
		},
		{
			policy: MisfireCatchUpLimited, overlap: OverlapForbid, overlapLimit: 1, maxCatchUp: 3,
			wantCreated: 1, wantSkipped: 2, wantSkipCode: "OVERLAP_FORBIDDEN",
			wantLastRun: true, wantLastEqual: now.Add(30 * time.Minute),
		},
	}
	for _, test := range tests {
		t.Run(string(test.policy)+"/"+string(test.overlap), func(t *testing.T) {
			repository, tasks, closeDatabase := newSchedulerRepository(t)
			defer closeDatabase()
			clock := newJumpClock(now)
			request := faultIntervalRequest(now, "sleep-"+string(test.policy), test.policy, test.maxCatchUp)
			request.OverlapPolicy = test.overlap
			request.OverlapLimit = test.overlapLimit
			schedule, err := repository.Create(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if schedule.NextRunAt == nil || !schedule.NextRunAt.Equal(now) {
				t.Fatalf("创建后游标错误: %#v", schedule)
			}
			clock.Jump(time.Hour)
			engine := newFaultEngine(t, repository, clock.Now)
			if err := engine.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			advanced, err := repository.Get(context.Background(), schedule.ID)
			if err != nil || advanced.NextRunAt == nil || !advanced.NextRunAt.After(clock.Now()) {
				t.Fatalf("唤醒后下次触发不可解释: %#v err=%v", advanced, err)
			}
			if !advanced.NextRunAt.Equal(now.Add(75 * time.Minute)) {
				t.Fatalf("唤醒后 next_run_at=%s want=%s", advanced.NextRunAt, now.Add(75*time.Minute))
			}
			if test.wantLastRun {
				if advanced.LastRunAt == nil || !advanced.LastRunAt.Equal(test.wantLastEqual) {
					t.Fatalf("last_run_at 不可解释: %#v want=%s", advanced.LastRunAt, test.wantLastEqual)
				}
			} else if advanced.LastRunAt != nil {
				t.Fatalf("skip 不应写入 last_run_at: %#v", advanced.LastRunAt)
			}
			page, err := repository.ListOccurrences(context.Background(), OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 20})
			if err != nil {
				t.Fatal(err)
			}
			created, skipped := 0, 0
			seen := map[time.Time]struct{}{}
			var previous time.Time
			for i := len(page.Occurrences) - 1; i >= 0; i-- {
				item := page.Occurrences[i]
				if _, dup := seen[item.PlannedAt]; dup {
					t.Fatalf("重复 occurrence planned_at=%s", item.PlannedAt)
				}
				seen[item.PlannedAt] = struct{}{}
				if !previous.IsZero() && !item.PlannedAt.After(previous) {
					t.Fatalf("occurrence UTC 不单调: prev=%s curr=%s", previous, item.PlannedAt)
				}
				previous = item.PlannedAt
				switch item.State {
				case "created":
					created++
					if item.TaskID == "" || item.ReasonCode != "" {
						t.Fatalf("created occurrence 状态不可解释: %#v", item)
					}
					task, getErr := tasks.Get(context.Background(), item.TaskID)
					if getErr != nil || task.State != taskengine.StatePending {
						t.Fatalf("created task 状态错误: %#v err=%v", task, getErr)
					}
				case "skipped":
					skipped++
					if item.TaskID != "" || item.ReasonCode != test.wantSkipCode {
						t.Fatalf("skipped occurrence 状态不可解释: %#v", item)
					}
				default:
					t.Fatalf("未知 occurrence 状态: %#v", item)
				}
			}
			if created != test.wantCreated || skipped != test.wantSkipped || len(page.Occurrences) != test.wantCreated+test.wantSkipped {
				t.Fatalf("occurrence 计数错误: created=%d skipped=%d total=%d test=%#v", created, skipped, len(page.Occurrences), test)
			}
		})
	}
}

func TestEngineIANATimezoneSwitchUsesNewZoneAndKeepsUTCMonotonic(t *testing.T) {
	now := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	clock := newJumpClock(now)
	request := faultCronRequest(now, "America/New_York", "tz-switch")
	schedule, err := repository.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	nyNext := time.Date(2026, 8, 12, 13, 0, 0, 0, time.UTC)
	if schedule.NextRunAt == nil || !schedule.NextRunAt.Equal(nyNext) {
		t.Fatalf("纽约下次触发错误: %#v want=%s", schedule.NextRunAt, nyNext)
	}
	updated, err := repository.Update(context.Background(), UpdateRequest{
		ID: schedule.ID, ExpectedRevision: schedule.Revision,
		CreateRequest: faultCronRequest(clock.Now(), "Asia/Shanghai", "tz-switch"),
	})
	if err != nil {
		t.Fatal(err)
	}
	shanghaiNext := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	if updated.Timezone != "Asia/Shanghai" || updated.NextRunAt == nil || !updated.NextRunAt.Equal(shanghaiNext) {
		t.Fatalf("时区切换后下次触发未按新时区: %#v", updated)
	}
	if updated.NextRunAt.Location() != time.UTC || !updated.NextRunAt.After(now) {
		t.Fatalf("下次触发必须是 UTC 且晚于切换时刻: %s", updated.NextRunAt)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	local := updated.NextRunAt.In(shanghai)
	if local.Hour() != 9 || local.Minute() != 0 {
		t.Fatalf("新时区本地墙钟错误: %s", local)
	}

	clock.Jump(2 * time.Hour)
	engine := newFaultEngine(t, repository, clock.Now)
	if err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListOccurrences(context.Background(), OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 10})
	if err != nil || len(page.Occurrences) != 1 {
		t.Fatalf("时区切换后 occurrence 错误: %#v err=%v", page, err)
	}
	if !page.Occurrences[0].PlannedAt.Equal(shanghaiNext) {
		t.Fatalf("不应按旧时区触发: planned=%s", page.Occurrences[0].PlannedAt)
	}
	advanced, err := repository.Get(context.Background(), schedule.ID)
	if err != nil || advanced.NextRunAt == nil {
		t.Fatalf("切换后游标错误: %#v err=%v", advanced, err)
	}
	wantNext := time.Date(2026, 8, 13, 1, 0, 0, 0, time.UTC)
	if !advanced.NextRunAt.Equal(wantNext) || !advanced.NextRunAt.After(*updated.NextRunAt) || !advanced.NextRunAt.After(clock.Now()) {
		t.Fatalf("UTC 下次触发不单调: next=%s prev=%s now=%s", advanced.NextRunAt, updated.NextRunAt, clock.Now())
	}
	spec, err := ParseSpec(KindCron, "0 9 * * *", "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	following := spec.Next(*advanced.NextRunAt)
	if following.IsZero() || !following.After(*advanced.NextRunAt) || following.Location() != time.UTC {
		t.Fatalf("后续触发 UTC 不单调: %s -> %s", advanced.NextRunAt, following)
	}
}

func TestEngineCrashRestartRecoversExpiredLeaseWithoutDuplicateOccurrence(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	path := filepath.Join(root, "scheduler.db")
	database, repository, tasks := openFaultSchedulerDB(t, path)
	clock := newJumpClock(now)
	schedule, err := repository.Create(context.Background(), faultIntervalRequest(now, "crash-lease", MisfireRunOnce, 1))
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	engine := newFaultEngine(t, repository, clock.Now)
	if err := engine.RunOnce(context.Background()); err != nil {
		database.Close()
		t.Fatal(err)
	}
	page, err := repository.ListOccurrences(context.Background(), OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 10})
	if err != nil || len(page.Occurrences) != 1 || page.Occurrences[0].TaskID == "" {
		database.Close()
		t.Fatalf("崩溃前 occurrence 错误: %#v err=%v", page, err)
	}
	occurrenceID := page.Occurrences[0].ID
	taskID := page.Occurrences[0].TaskID
	leased, found, err := tasks.LeaseNext(context.Background(), taskengine.LeaseRequest{
		Owner: "worker-crash", Now: clock.Now(), Duration: 200 * time.Millisecond,
	})
	if err != nil || !found || leased.ID != taskID || leased.State != taskengine.StateLeased {
		database.Close()
		t.Fatalf("领取租约失败: task=%#v found=%v err=%v", leased, found, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	clock.Jump(2 * time.Second)
	database, repository, tasks = openFaultSchedulerDB(t, path)
	defer database.Close()
	recovered, err := tasks.RecoverExpired(context.Background(), clock.Now(), 10, func(taskengine.Task) bool { return true })
	if err != nil || recovered != 1 {
		t.Fatalf("租约过期恢复数量错误: count=%d err=%v", recovered, err)
	}
	task, err := tasks.Get(context.Background(), taskID)
	if err != nil || task.State != taskengine.StatePending || task.LeaseOwner != "" {
		t.Fatalf("崩溃恢复后任务状态错误: %#v err=%v", task, err)
	}
	restarted := newFaultEngine(t, repository, clock.Now)
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := repository.ListOccurrences(context.Background(), OccurrenceListRequest{ScheduleID: schedule.ID, Limit: 10})
	if err != nil || len(after.Occurrences) != 1 || after.Occurrences[0].ID != occurrenceID || after.Occurrences[0].TaskID != taskID {
		t.Fatalf("重启后重复创建 occurrence: %#v err=%v", after, err)
	}
	advanced, err := repository.Get(context.Background(), schedule.ID)
	if err != nil || advanced.NextRunAt == nil || !advanced.NextRunAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("重启后游标被错误推进: %#v err=%v", advanced, err)
	}
	if _, err := repository.ApplyPlan(context.Background(), schedule, Plan{
		Occurrences: []PlannedOccurrence{{PlannedAt: now, ScheduledAt: now}},
		NextRunAt:   timeRef(now.Add(15 * time.Minute)),
	}, clock.Now()); !errors.Is(err, ErrConflict) {
		t.Fatalf("旧 snapshot 重放必须冲突: %v", err)
	}
}

func TestEngineSleepWakePollUsesInjectedClockNotRealSleep(t *testing.T) {
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	clock := newJumpClock(now)
	if _, err := repository.Create(context.Background(), faultIntervalRequest(now, "poll-sleep", MisfireRunOnce, 1)); err != nil {
		t.Fatal(err)
	}
	engine := newFaultEngine(t, repository, clock.Now)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock.Jump(time.Hour)
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := repository.ListOccurrences(context.Background(), OccurrenceListRequest{ScheduleID: "schedule-1", Limit: 10})
		if err == nil && len(page.Occurrences) == 1 && page.Occurrences[0].State == "created" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("注入时钟唤醒后未触发: page=%#v err=%v", page, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func newFaultEngine(t *testing.T, repository Repository, now func() time.Time) *Engine {
	t.Helper()
	engine, err := NewEngine(repository, EngineOptions{
		PollInterval: 5 * time.Millisecond, MisfireGrace: time.Minute, BatchSize: 20, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func faultIntervalRequest(now time.Time, name string, policy MisfirePolicy, maxCatchUp int) CreateRequest {
	return CreateRequest{
		Name: name, Enabled: true, Kind: KindInterval,
		Expression: now.Format(time.RFC3339) + "/15m", Timezone: "UTC",
		MisfirePolicy: policy, OverlapPolicy: OverlapAllowLimited,
		MaxCatchUp: maxCatchUp, OverlapLimit: 16,
		TaskTemplate: json.RawMessage(`{"task_type":"test.echo","payload_version":1,"payload":{},"priority":0,"max_attempts":2}`),
		Now:          now,
	}
}

func faultCronRequest(now time.Time, timezone, name string) CreateRequest {
	return CreateRequest{
		Name: name, Enabled: true, Kind: KindCron, Expression: "0 9 * * *", Timezone: timezone,
		MisfirePolicy: MisfireRunOnce, OverlapPolicy: OverlapAllowLimited,
		MaxCatchUp: 1, OverlapLimit: 16,
		TaskTemplate: json.RawMessage(`{"task_type":"test.echo","payload_version":1,"payload":{},"priority":0,"max_attempts":2}`),
		Now:          now,
	}
}

func openFaultSchedulerDB(t *testing.T, path string) (*sql.DB, *SQLiteRepository, *taskengine.SQLiteRepository) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: path,
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
		t.Fatal(err)
	}
	if err := taskmigrations.ReconcileLegacy(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := schedulermigrations.ReconcileLegacy(context.Background(), database); err != nil {
		database.Close()
		t.Fatal(err)
	}
	ids := &schedulerSequenceIDs{}
	tasks := taskengine.NewSQLiteRepository(database, ids.Next)
	return database, NewSQLiteRepository(database, tasks, ids.Next), tasks
}
