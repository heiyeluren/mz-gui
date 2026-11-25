package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine"
)

type failingRepository struct{ Repository }

func (repository failingRepository) Due(context.Context, time.Time, int) ([]Schedule, error) {
	return nil, errors.New("temporary scheduler failure")
}

func TestEngineRunOnceCreatesDueTaskAndAdvancesSchedule(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	createdAt := time.Now().UTC().Truncate(time.Millisecond)
	schedule, err := repository.Create(context.Background(), validScheduleRequest(createdAt, "engine-due"))
	if err != nil {
		t.Fatal(err)
	}
	now := schedule.NextRunAt.Add(time.Second)
	engine := newSchedulerEngine(t, repository, func() time.Time { return now })
	if err := engine.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	advanced, err := repository.Get(context.Background(), schedule.ID)
	if err != nil || advanced.Revision != 2 || advanced.LastRunAt == nil || advanced.NextRunAt == nil || !advanced.NextRunAt.After(now) {
		t.Fatalf("调度未推进: value=%#v err=%v", advanced, err)
	}
	page, err := tasks.List(context.Background(), taskengine.ListRequest{Types: []string{"test.echo"}, Limit: 10})
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("到期任务错误: tasks=%#v err=%v", page, err)
	}
}

func TestConcurrentEnginesDoNotDuplicateOccurrence(t *testing.T) {
	repository, tasks, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	createdAt := time.Now().UTC().Truncate(time.Millisecond)
	schedule, err := repository.Create(context.Background(), validScheduleRequest(createdAt, "engine-race"))
	if err != nil {
		t.Fatal(err)
	}
	now := schedule.NextRunAt.Add(time.Second)
	first := newSchedulerEngine(t, repository, func() time.Time { return now })
	second := newSchedulerEngine(t, repository, func() time.Time { return now })
	var group sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, engine := range []*Engine{first, second} {
		group.Add(1)
		go func(engine *Engine) {
			defer group.Done()
			if err := engine.RunOnce(context.Background()); err != nil {
				errorsSeen <- err
			}
		}(engine)
	}
	group.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("多实例调度错误: %v", err)
	}
	page, err := tasks.List(context.Background(), taskengine.ListRequest{Limit: 10})
	if err != nil || len(page.Tasks) != 1 {
		t.Fatalf("重复 occurrence 创建了任务: page=%#v err=%v", page, err)
	}
	var occurrences int
	if err := repository.database.QueryRow(`SELECT COUNT(*) FROM mzfw_schedule_occurrences WHERE schedule_id = ?`, schedule.ID).Scan(&occurrences); err != nil || occurrences != 1 {
		t.Fatalf("occurrence 数 = %d err=%v", occurrences, err)
	}
}

func TestEngineLifecycleRespondsToCancellation(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	engine := newSchedulerEngine(t, repository, time.Now)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复启动错误 = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEngineValidatesOptionsAndContexts(t *testing.T) {
	if _, err := NewEngine(nil, EngineOptions{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 engine 错误 = %v", err)
	}
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	engine := newSchedulerEngine(t, repository, time.Now)
	var nilContext context.Context
	if err := engine.Close(context.Background()); err != nil {
		t.Fatalf("未启动关闭错误 = %v", err)
	}
	if err := engine.Start(nilContext); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil start 错误 = %v", err)
	}
	if err := engine.RunOnce(nilContext); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil run 错误 = %v", err)
	}
	if err := engine.Close(nilContext); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil close 错误 = %v", err)
	}
	if err := engine.Close(context.Background()); err != nil {
		t.Fatalf("未启动 engine 关闭错误 = %v", err)
	}
}

func TestEngineReportsBackgroundErrors(t *testing.T) {
	repository, _, closeDatabase := newSchedulerRepository(t)
	defer closeDatabase()
	reported := make(chan error, 1)
	engine, err := NewEngine(failingRepository{Repository: repository}, EngineOptions{
		PollInterval: 5 * time.Millisecond, MisfireGrace: time.Minute, BatchSize: 10,
		OnError: func(err error) { reported <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("错误回调为空")
		}
	case <-time.After(time.Second):
		t.Fatal("后台错误未报告")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := engine.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(ctx); err != nil {
		t.Fatalf("重复关闭错误: %v", err)
	}
}

func newSchedulerEngine(t *testing.T, repository Repository, now func() time.Time) *Engine {
	t.Helper()
	engine, err := NewEngine(repository, EngineOptions{
		PollInterval: 5 * time.Millisecond, MisfireGrace: time.Minute, BatchSize: 20, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
