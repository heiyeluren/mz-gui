package managedprocess

import (
	"testing"
	"time"
)

func TestBuildCheckpointPlanForRestartPreservesBoundedMetadata(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	snapshot := Snapshot{
		ProcessID:           "mz-gui-agentd",
		State:               StateBackoff,
		PID:                 42,
		Attempt:             4,
		RestartCount:        3,
		ConsecutiveFailures: 2,
		LastReasonCode:      ReasonRestartScheduled,
		NextRestartAt:       now.Add(time.Second),
		RestartHistory: []RestartRecord{
			{Sequence: 1, ScheduledAt: now.Add(-3 * time.Second), Delay: time.Second, ReasonCode: ReasonExitedFailure},
			{Sequence: 2, ScheduledAt: now.Add(-2 * time.Second), Delay: time.Second, ReasonCode: ReasonExitedFailure},
			{Sequence: 3, ScheduledAt: now.Add(-time.Second), Delay: time.Second, ReasonCode: ReasonRestartScheduled},
		},
	}

	plan, err := BuildCheckpointPlan(CheckpointPolicy{
		ProcessID:            "mz-gui-agentd",
		PersistBeforeRestart: true,
		MaxRestartHistory:    2,
	}, snapshot, CheckpointTriggerRestartScheduled)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != CheckpointActionSnapshotMetadata || !plan.PersistSnapshot || plan.Terminal {
		t.Fatalf("重启前 checkpoint 策略错误: %#v", plan)
	}
	if plan.Snapshot.ProcessID != "mz-gui-agentd" || len(plan.Snapshot.RestartHistory) != 2 || plan.Snapshot.RestartHistory[0].Sequence != 2 {
		t.Fatalf("重启 checkpoint 元数据不完整或未裁剪: %#v", plan.Snapshot)
	}
	plan.Snapshot.RestartHistory[0].ReasonCode = "tampered"
	if snapshot.RestartHistory[1].ReasonCode == "tampered" {
		t.Fatal("checkpoint plan 必须深拷贝快照，不能暴露原始状态")
	}
}

func TestBuildCheckpointPlanForTerminalStateRequiresDrain(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	plan, err := BuildCheckpointPlan(CheckpointPolicy{
		ProcessID:     "mz-gui-browser-host",
		PersistOnExit: true,
	}, Snapshot{
		ProcessID:      "mz-gui-browser-host",
		State:          StateStopped,
		ExitCode:       0,
		LastReasonCode: ReasonStopped,
		ExitedAt:       now,
	}, CheckpointTriggerExited)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != CheckpointActionSnapshotAndDrain || !plan.PersistSnapshot || !plan.DrainOutput || !plan.Terminal {
		t.Fatalf("终态 checkpoint 应持久化快照并 drain 输出: %#v", plan)
	}
}

func TestBuildCheckpointPlanRejectsMismatchedProcess(t *testing.T) {
	_, err := BuildCheckpointPlan(CheckpointPolicy{ProcessID: "mz-gui-agentd"}, Snapshot{
		ProcessID: "other", State: StateRunning,
	}, CheckpointTriggerReady)
	if err == nil {
		t.Fatal("checkpoint 策略必须拒绝进程身份不匹配")
	}
}
