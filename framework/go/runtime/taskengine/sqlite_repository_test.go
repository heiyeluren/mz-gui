package taskengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	taskmigrations "github.com/heiyeluren/mz-gui/framework/go/runtime/taskengine/migrations"
	frameworksqlite "github.com/heiyeluren/mz-gui/framework/go/storage/sqlite"
)

func TestSQLiteRepositoryCreateIsSemanticIdempotent(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	request := CreateRequest{
		Type: "test.echo", Priority: 5, PayloadVersion: 1,
		Payload: json.RawMessage(`{"message":"hello","count":1}`), IdempotencyKey: "request-1",
		RequiredCapabilities: []string{"task.write", "system.health", "task.write"}, MaxAttempts: 3, Now: now,
	}

	created, wasCreated, err := repository.Create(context.Background(), request)
	if err != nil || !wasCreated {
		t.Fatalf("首次创建失败: task=%#v created=%v err=%v", created, wasCreated, err)
	}
	request.Payload = json.RawMessage(`{"count":1,"message":"hello"}`)
	again, wasCreated, err := repository.Create(context.Background(), request)
	if err != nil || wasCreated || again.ID != created.ID || again.Revision != created.Revision {
		t.Fatalf("幂等复用失败: first=%#v again=%#v created=%v err=%v", created, again, wasCreated, err)
	}
	if created.RequiredCapabilitiesJSON != `["system.health","task.write"]` {
		t.Fatalf("capability 未规范化: %s", created.RequiredCapabilitiesJSON)
	}

	request.Payload = json.RawMessage(`{"message":"different"}`)
	if _, _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键异请求错误 = %v", err)
	}
}

func TestSQLiteRepositoryCreateValidatesBoundaries(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	valid := CreateRequest{
		Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "request-1", MaxAttempts: 1, Now: time.Now().UTC(),
	}
	tests := []struct {
		name   string
		mutate func(*CreateRequest)
	}{
		{name: "任务类型", mutate: func(r *CreateRequest) { r.Type = "" }},
		{name: "幂等键", mutate: func(r *CreateRequest) { r.IdempotencyKey = "" }},
		{name: "非法JSON", mutate: func(r *CreateRequest) { r.Payload = json.RawMessage(`{`) }},
		{name: "payload过大", mutate: func(r *CreateRequest) { r.Payload = json.RawMessage(`"` + string(make([]byte, MaxPayloadBytes)) + `"`) }},
		{name: "attempt为零", mutate: func(r *CreateRequest) { r.MaxAttempts = 0 }},
		{name: "attempt过大", mutate: func(r *CreateRequest) { r.MaxAttempts = MaxAttempts + 1 }},
		{name: "时间为空", mutate: func(r *CreateRequest) { r.Now = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.mutate(&request)
			if _, _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("非法请求错误 = %v", err)
			}
		})
	}
}

func TestSQLiteRepositoryPublicValidationAndTransactionalCancel(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	valid := CreateRequest{
		Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "public-validation", MaxAttempts: 1, Now: now,
	}
	if err := ValidateCreateRequest(valid); err != nil {
		t.Fatalf("公开校验拒绝有效请求: %v", err)
	}
	invalid := valid
	invalid.Payload = json.RawMessage(`{`)
	if err := ValidateCreateRequest(invalid); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("公开校验错误 = %v", err)
	}

	ids := &sequenceIDs{}
	compatibilityRepository := NewSQLiteRepository(repository.database, ids.Next)
	task, created, err := compatibilityRepository.Create(context.Background(), valid)
	if err != nil || !created {
		t.Fatalf("兼容构造器创建任务: created=%v err=%v", created, err)
	}
	transaction, err := repository.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := repository.RequestCancelInTransaction(context.Background(), transaction, task.ID, now.Add(time.Second))
	if err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if cancelled.State != StateCancelled || cancelled.CancelRequestedAt == nil {
		t.Fatalf("事务取消结果错误: %#v", cancelled)
	}
	if _, err := repository.RequestCancelInTransaction(context.Background(), nil, task.ID, now); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空事务错误 = %v", err)
	}
}

func TestSQLiteRepositoryLeaseUsesPriorityAndSchedule(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC().Truncate(time.Millisecond)
	createTask(t, repository, "low", 1, now, nil)
	createTask(t, repository, "high", 10, now.Add(time.Millisecond), nil)
	future := now.Add(time.Hour)
	createTask(t, repository, "future", 100, now.Add(2*time.Millisecond), &future)

	leased, found, err := repository.LeaseNext(context.Background(), LeaseRequest{
		Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute,
	})
	if err != nil || !found || leased.Priority != 10 || leased.State != StateLeased {
		t.Fatalf("领取顺序错误: task=%#v found=%v err=%v", leased, found, err)
	}
	if leased.LeaseExpiresAt == nil || !leased.LeaseExpiresAt.Equal(now.Add(time.Second+time.Minute)) {
		t.Fatalf("租约到期时间错误: %#v", leased.LeaseExpiresAt)
	}
}

func TestSQLiteRepositoryConcurrentLeaseHasSingleWinner(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	createTask(t, repository, "one", 1, now, nil)

	var group sync.WaitGroup
	var mu sync.Mutex
	winners := make([]string, 0, 1)
	errorsSeen := make([]error, 0)
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			task, found, err := repository.LeaseNext(context.Background(), LeaseRequest{
				Owner: fmt.Sprintf("worker-%d", index), Now: now.Add(time.Second), Duration: time.Minute,
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsSeen = append(errorsSeen, err)
			} else if found {
				winners = append(winners, task.LeaseOwner)
			}
		}(index)
	}
	group.Wait()
	if len(errorsSeen) != 0 || len(winners) != 1 {
		t.Fatalf("并发领取结果错误: winners=%v errors=%v", winners, errorsSeen)
	}
}

func TestSQLiteRepositoryStartCreatesAttemptAtomically(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	createTask(t, repository, "start", 1, now, nil)
	leased, _, err := repository.LeaseNext(context.Background(), LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	started, attempt, err := repository.Start(context.Background(), leased.ID, "worker-1", now.Add(2*time.Second))
	if err != nil || started.State != StateRunning || started.AttemptCount != 1 {
		t.Fatalf("启动失败: task=%#v attempt=%#v err=%v", started, attempt, err)
	}
	if attempt.TaskID != started.ID || attempt.Number != 1 || attempt.WorkerID != "worker-1" || attempt.ID == "" {
		t.Fatalf("attempt 错误: %#v", attempt)
	}
	if _, _, err := repository.Start(context.Background(), leased.ID, "worker-2", now.Add(3*time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("重复/错误 owner 启动错误 = %v", err)
	}
}

func TestSQLiteRepositoryOutputCursorIsStrictlyIncreasing(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	task := createTask(t, repository, "output", 1, now, nil)

	var group sync.WaitGroup
	var mu sync.Mutex
	cursors := make([]int64, 0, 20)
	errorsSeen := make([]error, 0)
	for index := 0; index < 20; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			output, err := repository.AppendOutput(context.Background(), task.ID, "stdout", []byte(fmt.Sprintf("line-%d", index)), now)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errorsSeen = append(errorsSeen, err)
			} else {
				cursors = append(cursors, output.Cursor)
			}
		}(index)
	}
	group.Wait()
	sort.Slice(cursors, func(i, j int) bool { return cursors[i] < cursors[j] })
	if len(errorsSeen) != 0 || len(cursors) != 20 {
		t.Fatalf("并发输出失败: cursors=%v errors=%v", cursors, errorsSeen)
	}
	for index, cursor := range cursors {
		if cursor != int64(index+1) {
			t.Fatalf("cursor 不连续: %v", cursors)
		}
	}
	page, err := repository.ReadOutput(context.Background(), task.ID, 10, 5)
	if err != nil || len(page) != 5 || page[0].Cursor != 11 || page[4].Cursor != 15 {
		t.Fatalf("游标续传错误: page=%#v err=%v", page, err)
	}
}

func TestSQLiteRepositoryCancellationWinsLateSuccess(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	now := time.Now().UTC()
	createTask(t, repository, "cancel", 1, now, nil)
	leased, _, _ := repository.LeaseNext(context.Background(), LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	started, _, err := repository.Start(context.Background(), leased.ID, "worker-1", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	cancelling, err := repository.RequestCancel(context.Background(), started.ID, now.Add(3*time.Second))
	if err != nil || cancelling.State != StateRunning || cancelling.CancelRequestedAt == nil {
		t.Fatalf("取消请求错误: task=%#v err=%v", cancelling, err)
	}
	finished, err := repository.Finish(context.Background(), started.ID, Command{
		Kind: CommandSucceed, Now: now.Add(4 * time.Second), LeaseOwner: "worker-1", ResultSummary: `{"ok":true}`,
	})
	if err != nil || finished.State != StateCancelled {
		t.Fatalf("迟到成功覆盖取消: task=%#v err=%v", finished, err)
	}
}

func TestSQLiteRepositoryGetAndEmptyLease(t *testing.T) {
	repository, closeDatabase := newTestRepositoryWithIDs(t, nil)
	defer closeDatabase()
	ctx := context.Background()
	if _, err := repository.Get(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空 task_id 错误 = %v", err)
	}
	if _, err := repository.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在任务错误 = %v", err)
	}
	now := time.Now().UTC()
	if _, found, err := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now, Duration: time.Minute}); err != nil || found {
		t.Fatalf("空队列领取结果错误: found=%v err=%v", found, err)
	}
	if _, _, err := repository.LeaseNext(ctx, LeaseRequest{Now: now, Duration: time.Minute}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法领取错误 = %v", err)
	}

	task := createTask(t, repository, "random-id", 1, now, nil)
	if task.ID == "" || !strings.HasPrefix(task.ID, "task_") {
		t.Fatalf("默认随机 task id 错误: %q", task.ID)
	}
	loaded, err := repository.Get(ctx, task.ID)
	if err != nil || loaded.ID != task.ID {
		t.Fatalf("读取任务失败: task=%#v err=%v", loaded, err)
	}
}

func TestSQLiteRepositoryImmediateCancelAndOutputBoundaries(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC()
	task := createTask(t, repository, "immediate-cancel", 1, now, nil)
	cancelled, err := repository.RequestCancel(ctx, task.ID, now.Add(time.Second))
	if err != nil || cancelled.State != StateCancelled || cancelled.FinishedAt == nil {
		t.Fatalf("pending 立即取消失败: task=%#v err=%v", cancelled, err)
	}

	if _, err := repository.AppendOutput(ctx, "missing", "stdout", []byte("line"), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在任务输出错误 = %v", err)
	}
	for _, test := range []struct {
		taskID  string
		kind    string
		payload []byte
	}{
		{taskID: "", kind: "stdout", payload: []byte("line")},
		{taskID: task.ID, kind: "bad kind", payload: []byte("line")},
		{taskID: task.ID, kind: "stdout", payload: nil},
		{taskID: task.ID, kind: "stdout", payload: make([]byte, MaxOutputChunkBytes+1)},
	} {
		if _, err := repository.AppendOutput(ctx, test.taskID, test.kind, test.payload, now); err == nil {
			t.Fatalf("非法输出必须拒绝: %#v", test)
		}
	}
	for _, test := range []struct {
		taskID string
		after  int64
		limit  int
	}{
		{taskID: "", limit: 1}, {taskID: task.ID, after: -1, limit: 1},
		{taskID: task.ID, limit: 0}, {taskID: task.ID, limit: MaxOutputPage + 1},
	} {
		if _, err := repository.ReadOutput(ctx, test.taskID, test.after, test.limit); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法分页错误 = %v", err)
		}
	}
}

func TestSQLiteRepositoryFinishFailureAndValidation(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC()
	createTask(t, repository, "failure", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	started, _, err := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Finish(ctx, started.ID, Command{Kind: CommandStart, Now: now}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 finish 命令错误 = %v", err)
	}
	if _, err := repository.Finish(ctx, started.ID, Command{
		Kind: CommandFail, Now: now, LeaseOwner: "worker-1", ErrorSummary: string(make([]byte, MaxResultBytes+1)),
	}); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("过大错误摘要错误 = %v", err)
	}
	retryAt := now.Add(time.Minute)
	retrying, err := repository.Finish(ctx, started.ID, Command{
		Kind: CommandFail, Now: now.Add(3 * time.Second), LeaseOwner: "worker-1",
		Retryable: true, RetryAt: retryAt, ErrorCode: "UNAVAILABLE", ErrorSummary: "temporary",
	})
	if err != nil || retrying.State != StateRetryWait || retrying.NextRetryAt == nil {
		t.Fatalf("失败重试落库错误: task=%#v err=%v", retrying, err)
	}
}

func TestSQLiteRepositoryRejectsInvalidCapabilities(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	request := CreateRequest{
		Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "invalid-capability", MaxAttempts: 1, Now: time.Now().UTC(),
		RequiredCapabilities: []string{"bad capability"},
	}
	if _, _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 capability 错误 = %v", err)
	}
	request.IdempotencyKey = "too-many-capabilities"
	request.RequiredCapabilities = make([]string, 65)
	for index := range request.RequiredCapabilities {
		request.RequiredCapabilities[index] = fmt.Sprintf("cap.%d", index)
	}
	if _, _, err := repository.Create(context.Background(), request); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("过多 capability 错误 = %v", err)
	}
}

func TestSQLiteRepositoryRenewsLeaseAndPromotesDueRetry(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	createTask(t, repository, "retry-due", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	renewed, err := repository.RenewLease(ctx, leased.ID, "worker-1", now.Add(2*time.Second), 2*time.Minute)
	if err != nil || renewed.LeaseExpiresAt == nil || !renewed.LeaseExpiresAt.Equal(now.Add(2*time.Second+2*time.Minute)) {
		t.Fatalf("续租失败: task=%#v err=%v", renewed, err)
	}
	started, _, err := repository.Start(ctx, renewed.ID, "worker-1", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	retryAt := now.Add(10 * time.Second)
	retrying, err := repository.Finish(ctx, started.ID, Command{
		Kind: CommandFail, Now: now.Add(4 * time.Second), LeaseOwner: "worker-1",
		Retryable: true, RetryAt: retryAt, ErrorCode: "UNAVAILABLE",
	})
	if err != nil || retrying.State != StateRetryWait {
		t.Fatalf("进入 retry_wait 失败: task=%#v err=%v", retrying, err)
	}
	if count, err := repository.PromoteDueRetries(ctx, retryAt.Add(-time.Millisecond), 10); err != nil || count != 0 {
		t.Fatalf("提前提升了重试: count=%d err=%v", count, err)
	}
	if count, err := repository.PromoteDueRetries(ctx, retryAt, 10); err != nil || count != 1 {
		t.Fatalf("到期重试未提升: count=%d err=%v", count, err)
	}
	pending, err := repository.Get(ctx, started.ID)
	if err != nil || pending.State != StatePending || pending.NextRetryAt != nil {
		t.Fatalf("重试提升结果错误: task=%#v err=%v", pending, err)
	}
}

func TestSQLiteRepositoryRecoversExpiredTasksByReplayPolicy(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	createWithType := func(taskType, key string) Task {
		task, _, err := repository.Create(ctx, CreateRequest{
			Type: taskType, PayloadVersion: 1, Payload: json.RawMessage(`{}`),
			IdempotencyKey: key, MaxAttempts: 2, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	createWithType("test.resumable", "resumable")
	resumableLease, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now, Duration: time.Second})
	resumable, _, _ := repository.Start(ctx, resumableLease.ID, "worker-1", now.Add(time.Millisecond))
	createWithType("test.unsafe", "unsafe")
	unsafeLease, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-2", Now: now, Duration: time.Second})
	unsafe, _, _ := repository.Start(ctx, unsafeLease.ID, "worker-2", now.Add(time.Millisecond))
	createWithType("test.leased", "leased-only")
	leasedOnly, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-3", Now: now, Duration: time.Second})

	recovered, err := repository.RecoverExpired(ctx, now.Add(2*time.Second), 10, func(task Task) bool {
		return task.Type == "test.resumable"
	})
	if err != nil || recovered != 3 {
		t.Fatalf("恢复数量错误: count=%d err=%v", recovered, err)
	}
	assertTaskState(t, repository, resumable.ID, StatePending)
	assertTaskState(t, repository, unsafe.ID, StateManualReview)
	assertTaskState(t, repository, leasedOnly.ID, StatePending)
	var errorCode, errorSummary string
	if err := repository.database.QueryRowContext(ctx, `SELECT error_code, error_summary FROM mzfw_task_attempts WHERE task_id = ?`, unsafe.ID).Scan(&errorCode, &errorSummary); err != nil {
		t.Fatalf("读取恢复诊断: %v", err)
	}
	if errorCode != "WORKER_INTERRUPTED" || errorSummary != "任务工作进程在执行期间停止" {
		t.Fatalf("Framework 恢复诊断不应包含产品语义: code=%q summary=%q", errorCode, errorSummary)
	}
}

func TestSQLiteRepositoryUsesConfiguredRecoveryDiagnostics(t *testing.T) {
	ids := &sequenceIDs{}
	repository, closeDatabase := newTestRepositoryWithOptions(t, SQLiteRepositoryOptions{
		IDSource:             ids.Next,
		RecoveryErrorCode:    "CUSTOM_INTERRUPTED",
		RecoveryErrorSummary: "调用方定义的中断说明",
	})
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task := createTask(t, repository, "custom-recovery", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now, Duration: time.Second})
	running, _, _ := repository.Start(ctx, leased.ID, "worker-1", now.Add(time.Millisecond))
	if _, err := repository.RecoverExpired(ctx, now.Add(2*time.Second), 10, func(Task) bool { return false }); err != nil {
		t.Fatal(err)
	}
	var errorCode, errorSummary string
	if err := repository.database.QueryRowContext(ctx, `SELECT error_code, error_summary FROM mzfw_task_attempts WHERE task_id = ?`, running.ID).Scan(&errorCode, &errorSummary); err != nil {
		t.Fatal(err)
	}
	if errorCode != "CUSTOM_INTERRUPTED" || errorSummary != "调用方定义的中断说明" {
		t.Fatalf("调用方恢复诊断未生效: code=%q summary=%q task=%s", errorCode, errorSummary, task.ID)
	}
}

func TestSQLiteRepositoryListsWithStableCursorAndFilters(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	for index := 0; index < 5; index++ {
		task, _, err := repository.Create(ctx, CreateRequest{
			Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
			IdempotencyKey: fmt.Sprintf("list-%d", index), WorkspaceID: "workspace-a",
			MaxAttempts: 1, Now: base.Add(time.Duration(index) * time.Millisecond),
		})
		if err != nil || task.ID == "" {
			t.Fatalf("创建分页任务: task=%#v err=%v", task, err)
		}
	}
	createTypedTask(t, repository, "test.other", "list-other", 1, base.Add(10*time.Millisecond))

	first, err := repository.List(ctx, ListRequest{
		States: []State{StatePending}, Types: []string{"test.echo"}, WorkspaceID: "workspace-a", Limit: 2,
	})
	if err != nil || len(first.Tasks) != 2 || !first.HasMore || first.NextCreatedAt == nil || first.NextTaskID == "" {
		t.Fatalf("首个分页错误: page=%#v err=%v", first, err)
	}
	second, err := repository.List(ctx, ListRequest{
		States: []State{StatePending}, Types: []string{"test.echo"}, WorkspaceID: "workspace-a", Limit: 2,
		BeforeCreatedAt: first.NextCreatedAt, BeforeTaskID: first.NextTaskID,
	})
	if err != nil || len(second.Tasks) != 2 || !second.HasMore {
		t.Fatalf("第二个分页错误: page=%#v err=%v", second, err)
	}
	third, err := repository.List(ctx, ListRequest{
		States: []State{StatePending}, Types: []string{"test.echo"}, WorkspaceID: "workspace-a", Limit: 2,
		BeforeCreatedAt: second.NextCreatedAt, BeforeTaskID: second.NextTaskID,
	})
	if err != nil || len(third.Tasks) != 1 || third.HasMore {
		t.Fatalf("末页错误: page=%#v err=%v", third, err)
	}
	seen := map[string]struct{}{}
	for _, page := range []TaskPage{first, second, third} {
		for _, task := range page.Tasks {
			if _, exists := seen[task.ID]; exists {
				t.Fatalf("分页重复任务: %s", task.ID)
			}
			seen[task.ID] = struct{}{}
		}
	}
	if len(seen) != 5 {
		t.Fatalf("分页任务数 = %d", len(seen))
	}
}

func TestSQLiteRepositoryManualRetryPersistsAndPreservesAttempts(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task, _, err := repository.Create(ctx, CreateRequest{
		Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "manual-retry", MaxAttempts: 1, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	started, _, _ := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	failed, err := repository.Finish(ctx, started.ID, Command{
		Kind: CommandFail, Now: now.Add(3 * time.Second), LeaseOwner: "worker-1", ErrorCode: "FAILED",
	})
	if err != nil || failed.State != StateFailed {
		t.Fatalf("准备失败任务: task=%#v err=%v", failed, err)
	}
	retried, err := repository.Retry(ctx, task.ID, now.Add(4*time.Second))
	if err != nil || retried.State != StatePending || retried.MaxAttempts != 2 || retried.AttemptCount != 1 {
		t.Fatalf("人工重试失败: task=%#v err=%v", retried, err)
	}
	var attempts int
	if err := repository.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM mzfw_task_attempts WHERE task_id = ?`, task.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatalf("历史 attempt 被破坏: count=%d err=%v", attempts, err)
	}
	if _, err := repository.Retry(ctx, task.ID, now.Add(5*time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pending 重复人工重试错误 = %v", err)
	}
}

func TestSQLiteRepositoryRejectsInvalidListAndResultJSON(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, request := range []ListRequest{
		{Limit: 0},
		{Limit: MaxTaskPage + 1},
		{Limit: 1, States: []State{"unknown"}},
		{Limit: 1, Types: []string{"bad type"}},
		{Limit: 1, BeforeTaskID: "task-1"},
		{Limit: 1, BeforeCreatedAt: &now},
	} {
		if _, err := repository.List(ctx, request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("非法分页未拒绝: request=%#v err=%v", request, err)
		}
	}
	createTask(t, repository, "invalid-result", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	started, _, _ := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	if _, err := repository.Finish(ctx, started.ID, Command{
		Kind: CommandSucceed, Now: now.Add(3 * time.Second), LeaseOwner: "worker-1", ResultSummary: "not-json",
	}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法结果 JSON 错误 = %v", err)
	}
}

func TestSQLiteRepositoryPersistsIdempotentCancellation(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	first := createTask(t, repository, "cancel-idempotent-1", 1, now, nil)
	second := createTask(t, repository, "cancel-idempotent-2", 1, now.Add(time.Millisecond), nil)
	cancelled, applied, err := repository.RequestCancelIdempotent(ctx, first.ID, "用户取消", "cancel-key-1", now.Add(time.Second))
	if err != nil || !applied || cancelled.State != StateCancelled {
		t.Fatalf("首次幂等取消失败: task=%#v applied=%v err=%v", cancelled, applied, err)
	}
	replayed, applied, err := repository.RequestCancelIdempotent(ctx, first.ID, "用户取消", "cancel-key-1", now.Add(2*time.Second))
	if err != nil || applied || replayed.State != StateCancelled || replayed.Revision != cancelled.Revision {
		t.Fatalf("重放幂等取消失败: task=%#v applied=%v err=%v", replayed, applied, err)
	}
	if _, _, err := repository.RequestCancelIdempotent(ctx, first.ID, "不同原因", "cancel-key-1", now.Add(3*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键不同语义错误 = %v", err)
	}
	if _, _, err := repository.RequestCancelIdempotent(ctx, second.ID, "用户取消", "cancel-key-1", now.Add(3*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键不同任务错误 = %v", err)
	}
}

func TestSQLiteRepositoryPersistsIdempotentManualRetry(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	task, _, err := repository.Create(ctx, CreateRequest{
		Type: "test.echo", PayloadVersion: 1, Payload: json.RawMessage(`{}`),
		IdempotencyKey: "retry-idempotent-task", MaxAttempts: 1, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	running, _, _ := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	failed, err := repository.Finish(ctx, running.ID, Command{
		Kind: CommandFail, Now: now.Add(3 * time.Second), LeaseOwner: "worker-1", ErrorCode: "FAILED",
	})
	if err != nil || failed.State != StateFailed {
		t.Fatal("准备失败任务错误")
	}
	retried, applied, err := repository.RetryIdempotent(ctx, task.ID, "用户重试", "retry-key-1", now.Add(4*time.Second))
	if err != nil || !applied || retried.State != StatePending {
		t.Fatalf("首次幂等重试失败: task=%#v applied=%v err=%v", retried, applied, err)
	}
	replayed, applied, err := repository.RetryIdempotent(ctx, task.ID, "用户重试", "retry-key-1", now.Add(5*time.Second))
	if err != nil || applied || replayed.State != StatePending || replayed.Revision != retried.Revision {
		t.Fatalf("重放幂等重试失败: task=%#v applied=%v err=%v", replayed, applied, err)
	}
}

func assertTaskState(t *testing.T, repository *SQLiteRepository, taskID string, state State) {
	t.Helper()
	task, err := repository.Get(context.Background(), taskID)
	if err != nil || task.State != state || task.LeaseOwner != "" {
		t.Fatalf("任务 %s 状态错误: task=%#v err=%v", taskID, task, err)
	}
}

func newTestRepository(t *testing.T) (*SQLiteRepository, func()) {
	return newTestRepositoryWithIDs(t, (&sequenceIDs{}).Next)
}

func newTestRepositoryWithIDs(t *testing.T, ids IDSource) (*SQLiteRepository, func()) {
	return newTestRepositoryWithOptions(t, SQLiteRepositoryOptions{IDSource: ids})
}

func newTestRepositoryWithOptions(t *testing.T, options SQLiteRepositoryOptions) (*SQLiteRepository, func()) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "framework-taskrepo-")
	if err != nil {
		t.Fatal(err)
	}
	database, err := frameworksqlite.Open(context.Background(), frameworksqlite.Options{
		Path: filepath.Join(root, "taskengine.db"),
		Bundles: []frameworksqlite.MigrationBundle{
			taskmigrations.Bundle(),
			{
				Owner: "application.taskengine-test", Dependencies: []string{taskmigrations.Owner},
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
	return NewSQLiteRepositoryWithOptions(database, options), func() {
		database.Close()
		os.RemoveAll(root)
	}
}

func createTask(t *testing.T, repository *SQLiteRepository, key string, priority int, now time.Time, scheduledAt *time.Time) Task {
	t.Helper()
	task, created, err := repository.Create(context.Background(), CreateRequest{
		Type: "test.echo", Priority: priority, PayloadVersion: 1, Payload: json.RawMessage(`{"key":"` + key + `"}`),
		IdempotencyKey: key, RequiredCapabilities: []string{"task.write"}, ScheduledAt: scheduledAt, MaxAttempts: 2, Now: now,
	})
	if err != nil || !created {
		t.Fatalf("创建测试任务 %s: created=%v err=%v", key, created, err)
	}
	return task
}

type sequenceIDs struct {
	mu     sync.Mutex
	serial int
}

func (ids *sequenceIDs) Next(prefix string) string {
	ids.mu.Lock()
	defer ids.mu.Unlock()
	ids.serial++
	return fmt.Sprintf("%s-%d", prefix, ids.serial)
}
