package taskengine

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSQLiteRepositoryCheckpointIsAtomicIdempotentAndMonotonic(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	createTask(t, repository, "checkpoint", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	running, attempt, err := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	request := CheckpointRequest{
		TaskID: running.ID, AttemptNo: attempt.Number, Key: "downloaded",
		Payload: json.RawMessage(`{"offset":42}`), Now: now.Add(3 * time.Second),
	}
	first, created, err := repository.SaveCheckpoint(ctx, request)
	if err != nil || !created || first.Revision != 1 || first.OutputCursor != 1 || first.PayloadJSON != `{"offset":42}` {
		t.Fatalf("首次 checkpoint 错误: checkpoint=%#v created=%v err=%v", first, created, err)
	}
	replayed, created, err := repository.SaveCheckpoint(ctx, request)
	if err != nil || created || replayed.Revision != first.Revision || replayed.OutputCursor != first.OutputCursor {
		t.Fatalf("checkpoint 幂等重放错误: checkpoint=%#v created=%v err=%v", replayed, created, err)
	}
	conflict := request
	conflict.Payload = json.RawMessage(`{"offset":43}`)
	if _, _, err := repository.SaveCheckpoint(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("同键异 checkpoint 错误 = %v", err)
	}

	secondRequest := request
	secondRequest.Key = "verified"
	secondRequest.Payload = json.RawMessage(`{"sha256":"ok"}`)
	secondRequest.Now = now.Add(4 * time.Second)
	second, created, err := repository.SaveCheckpoint(ctx, secondRequest)
	if err != nil || !created || second.Revision != 2 || second.OutputCursor != 2 {
		t.Fatalf("第二个 checkpoint 错误: checkpoint=%#v created=%v err=%v", second, created, err)
	}
	latest, found, err := repository.LatestCheckpoint(ctx, running.ID)
	if err != nil || !found || latest.Key != "verified" || latest.Revision != 2 {
		t.Fatalf("读取最新 checkpoint 错误: checkpoint=%#v found=%v err=%v", latest, found, err)
	}
	outputs, err := repository.ReadOutput(ctx, running.ID, 0, 10)
	if err != nil || len(outputs) != 2 || outputs[0].Kind != "checkpoint" || outputs[1].Cursor != 2 {
		t.Fatalf("checkpoint 进度流错误: outputs=%#v err=%v", outputs, err)
	}
}

func TestSQLiteRepositoryCheckpointValidatesTaskAttemptAndPayload(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC()
	task := createTask(t, repository, "checkpoint-validation", 1, now, nil)
	valid := CheckpointRequest{
		TaskID: task.ID, AttemptNo: 1, Key: "step", Payload: json.RawMessage(`{}`), Now: now,
	}
	if _, _, err := repository.SaveCheckpoint(ctx, valid); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("非 running 任务 checkpoint 错误 = %v", err)
	}
	invalid := []CheckpointRequest{
		{},
		{TaskID: task.ID, AttemptNo: 0, Key: "step", Payload: json.RawMessage(`{}`), Now: now},
		{TaskID: task.ID, AttemptNo: 1, Key: "bad key", Payload: json.RawMessage(`{}`), Now: now},
		{TaskID: task.ID, AttemptNo: 1, Key: "step", Payload: json.RawMessage(`{`), Now: now},
		{TaskID: task.ID, AttemptNo: 1, Key: "step", Payload: json.RawMessage(`{}`)},
	}
	for index, request := range invalid {
		if _, _, err := repository.SaveCheckpoint(ctx, request); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("第 %d 个非法 checkpoint 错误 = %v", index, err)
		}
	}
	if _, found, err := repository.LatestCheckpoint(ctx, task.ID); err != nil || found {
		t.Fatalf("空 checkpoint 结果错误: found=%v err=%v", found, err)
	}
	if _, _, err := repository.LatestCheckpoint(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("空 task_id 查询错误 = %v", err)
	}
	if _, _, err := repository.LatestCheckpoint(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在任务查询错误 = %v", err)
	}
}

func TestSQLiteRepositoryCheckpointFailureRollsBackProgressOutput(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	ctx := context.Background()
	now := time.Now().UTC()
	createTask(t, repository, "checkpoint-rollback", 1, now, nil)
	leased, _, _ := repository.LeaseNext(ctx, LeaseRequest{Owner: "worker-1", Now: now.Add(time.Second), Duration: time.Minute})
	running, attempt, err := repository.Start(ctx, leased.ID, "worker-1", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.Exec(`CREATE TRIGGER fail_checkpoint_insert
		BEFORE INSERT ON mzfw_task_checkpoints BEGIN SELECT RAISE(ABORT, 'injected checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	request := CheckpointRequest{
		TaskID: running.ID, AttemptNo: attempt.Number, Key: "step",
		Payload: json.RawMessage(`{"done":true}`), Now: now.Add(3 * time.Second),
	}
	if _, _, err := repository.SaveCheckpoint(ctx, request); err == nil {
		t.Fatal("故障注入必须使 checkpoint 失败")
	}
	var outputs int
	if err := repository.database.QueryRow("SELECT COUNT(*) FROM mzfw_task_output WHERE task_id = ?", running.ID).Scan(&outputs); err != nil || outputs != 0 {
		t.Fatalf("失败 checkpoint 的进度输出未回滚: count=%d err=%v", outputs, err)
	}
	if _, err := repository.database.Exec("DROP TRIGGER fail_checkpoint_insert"); err != nil {
		t.Fatal(err)
	}
	checkpoint, created, err := repository.SaveCheckpoint(ctx, request)
	if err != nil || !created || checkpoint.Revision != 1 || checkpoint.OutputCursor != 1 {
		t.Fatalf("故障解除后 checkpoint 游标错误: checkpoint=%#v created=%v err=%v", checkpoint, created, err)
	}
}

func TestEngineResumesFromCommittedCheckpointAfterRestart(t *testing.T) {
	repository, closeDatabase := newTestRepository(t)
	defer closeDatabase()
	registry := NewRegistry()
	checkpointSaved := make(chan struct{})
	resumed := make(chan Checkpoint, 1)
	var calls atomic.Int32
	mustRegister(t, registry, "test.checkpoint-resume", Registration{
		Resumable: true, Timeout: 5 * time.Second,
		Handler: HandlerFunc(func(ctx context.Context, _ Task, output OutputWriter) ExecutionResult {
			checkpoints, ok := output.(CheckpointWriter)
			if !ok {
				return ExecutionResult{ErrorCode: "CHECKPOINT_UNAVAILABLE"}
			}
			if calls.Add(1) == 1 {
				if _, _, err := checkpoints.SaveCheckpoint(ctx, "downloaded", json.RawMessage(`{"offset":42}`)); err != nil {
					return ExecutionResult{ErrorCode: "CHECKPOINT_FAILED", ErrorSummary: err.Error()}
				}
				close(checkpointSaved)
				<-ctx.Done()
				return ExecutionResult{ErrorCode: "INTERRUPTED"}
			}
			checkpoint, found, err := checkpoints.LatestCheckpoint(ctx)
			if err != nil || !found {
				return ExecutionResult{ErrorCode: "CHECKPOINT_MISSING"}
			}
			resumed <- checkpoint
			return ExecutionResult{Summary: json.RawMessage(`{"resumed":true}`)}
		}),
	})
	task := createTypedTask(t, repository, "test.checkpoint-resume", "checkpoint-restart", 1, time.Now().UTC())

	first := newTestEngine(t, repository, registry, 1)
	startEngine(t, first)
	select {
	case <-checkpointSaved:
	case <-time.After(2 * time.Second):
		t.Fatal("首次执行未提交 checkpoint")
	}
	closeEngine(t, first)

	second := newTestEngine(t, repository, registry, 1)
	startEngine(t, second)
	select {
	case checkpoint := <-resumed:
		if checkpoint.Key != "downloaded" || checkpoint.PayloadJSON != `{"offset":42}` {
			t.Fatalf("恢复 checkpoint 错误: %#v", checkpoint)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("重启后未从 checkpoint 恢复")
	}
	finished := waitTaskState(t, repository, task.ID, StateSucceeded, 2*time.Second)
	if finished.AttemptCount != 2 || finished.ResultSummaryJSON != `{"resumed":true}` {
		t.Fatalf("恢复任务终态错误: %#v", finished)
	}
	outputs, err := repository.ReadOutput(context.Background(), task.ID, 0, 10)
	if err != nil || len(outputs) != 1 || outputs[0].Kind != "checkpoint" {
		t.Fatalf("重启后 checkpoint 输出重复: outputs=%#v err=%v", outputs, err)
	}
}
