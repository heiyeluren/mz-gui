package taskengine

import (
	"context"
	"testing"
	"time"
)

func TestEngineRecoversLeaseExpiringAfterStartup(t *testing.T) {
	repo, closeDB := newTestRepository(t)
	defer closeDB()
	now := time.Now().UTC()
	task := createTask(t, repo, "late-expiry", 1, now, nil)
	leased, ok, err := repo.LeaseNext(context.Background(), LeaseRequest{Owner: "old-process", Now: now, Duration: 300 * time.Millisecond})
	if err != nil || !ok {
		t.Fatalf("lease: %v", err)
	}
	if _, _, err := repo.Start(context.Background(), leased.ID, "old-process", now); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	mustRegister(t, registry, "test.echo", Registration{Handler: HandlerFunc(func(context.Context, Task, OutputWriter) ExecutionResult {
		t.Error("不可恢复任务不得重跑")
		return ExecutionResult{}
	}), Timeout: time.Second})
	engine := newTestEngine(t, repo, registry, 1)
	startEngine(t, engine)
	waitTaskState(t, repo, task.ID, StateManualReview, 2*time.Second)
}

func TestReconcileSuccessRequiresDormantTaskAndMatchingRevision(t *testing.T) {
	now := time.Now().UTC()
	expiry := now.Add(-time.Second)
	task := Task{ID: "receipt", State: StateRunning, Revision: 3, LeaseOwner: "old", LeaseExpiresAt: &expiry, AttemptCount: 1}
	cmd := Command{Kind: CommandReconcileSuccess, Now: now, ExpectedRevision: 3, ResultSummary: `{"version":"1.0.0"}`}
	result, err := Apply(task, cmd)
	if err != nil || result.State != StateSucceeded || result.LeaseOwner != "" {
		t.Fatalf("收口失败: %#v %v", result, err)
	}
	cmd.ExpectedRevision = 2
	if _, err := Apply(task, cmd); err == nil {
		t.Fatal("不得覆盖并发变更")
	}
	cmd.ExpectedRevision = 3
	future := now.Add(time.Minute)
	task.LeaseExpiresAt = &future
	if _, err := Apply(task, cmd); err == nil {
		t.Fatal("不得抢占有效租约")
	}
	task.State = StateManualReview
	task.LeaseExpiresAt = nil
	task.ErrorCode = "WORKER_INTERRUPTED"
	result, err = Apply(task, cmd)
	if err != nil || result.ErrorCode != "" {
		t.Fatalf("人工检查态未收口: %#v %v", result, err)
	}
	task.CancelRequestedAt = &now
	if _, err := Apply(task, cmd); err == nil {
		t.Fatal("取消任务不得标成功")
	}
}
