package managedprocess

import "time"

type CheckpointTrigger string

const (
	CheckpointTriggerStarting         CheckpointTrigger = "process.starting"
	CheckpointTriggerReady            CheckpointTrigger = "process.ready"
	CheckpointTriggerExited           CheckpointTrigger = "process.exited"
	CheckpointTriggerRestartScheduled CheckpointTrigger = "process.restart_scheduled"
	CheckpointTriggerCrashLoop        CheckpointTrigger = "process.crash_loop"
	CheckpointTriggerStopRequested    CheckpointTrigger = "process.stop_requested"
)

type CheckpointAction string

const (
	CheckpointActionNone             CheckpointAction = "none"
	CheckpointActionSnapshotMetadata CheckpointAction = "snapshot_metadata"
	CheckpointActionSnapshotAndDrain CheckpointAction = "snapshot_and_drain"
)

type CheckpointPolicy struct {
	ProcessID            string
	PersistOnStart       bool
	PersistOnReady       bool
	PersistOnExit        bool
	PersistBeforeRestart bool
	PersistOnCrashLoop   bool
	PersistOnStop        bool
	MaxRestartHistory    int
}

type CheckpointPlan struct {
	ProcessID       string
	Trigger         CheckpointTrigger
	Action          CheckpointAction
	Snapshot        Snapshot
	PersistSnapshot bool
	DrainOutput     bool
	Terminal        bool
	ReasonCode      string
	CreatedAt       time.Time
}

func BuildCheckpointPlan(policy CheckpointPolicy, snapshot Snapshot, trigger CheckpointTrigger) (CheckpointPlan, error) {
	processID := policy.ProcessID
	if processID == "" {
		processID = snapshot.ProcessID
	}
	if processID == "" || snapshot.ProcessID == "" || processID != snapshot.ProcessID || !validCheckpointTrigger(trigger) {
		return CheckpointPlan{}, ErrInvalidSpec
	}
	action := CheckpointActionNone
	persist := checkpointEnabled(policy, trigger)
	terminal := checkpointTerminal(snapshot, trigger)
	drain := false
	if persist {
		action = CheckpointActionSnapshotMetadata
		if terminal {
			action = CheckpointActionSnapshotAndDrain
			drain = true
		}
	}
	cloned := snapshot.clone()
	cloned.RestartHistory = trimRestartHistory(cloned.RestartHistory, policy.MaxRestartHistory)
	return CheckpointPlan{
		ProcessID:       processID,
		Trigger:         trigger,
		Action:          action,
		Snapshot:        cloned,
		PersistSnapshot: persist,
		DrainOutput:     drain,
		Terminal:        terminal,
		ReasonCode:      cloned.LastReasonCode,
		CreatedAt:       checkpointObservedAt(cloned),
	}, nil
}

func checkpointEnabled(policy CheckpointPolicy, trigger CheckpointTrigger) bool {
	switch trigger {
	case CheckpointTriggerStarting:
		return policy.PersistOnStart
	case CheckpointTriggerReady:
		return policy.PersistOnReady
	case CheckpointTriggerExited:
		return policy.PersistOnExit
	case CheckpointTriggerRestartScheduled:
		return policy.PersistBeforeRestart
	case CheckpointTriggerCrashLoop:
		return policy.PersistOnCrashLoop
	case CheckpointTriggerStopRequested:
		return policy.PersistOnStop
	default:
		return false
	}
}

func checkpointTerminal(snapshot Snapshot, trigger CheckpointTrigger) bool {
	switch trigger {
	case CheckpointTriggerExited, CheckpointTriggerCrashLoop:
		return true
	case CheckpointTriggerStopRequested:
		return snapshot.State == StateStopping || snapshot.State == StateStopped || snapshot.State == StateDisabled
	default:
		return snapshot.State == StateStopped || snapshot.State == StateDisabled || snapshot.State == StateCrashLoop || snapshot.State == StateIncompatible
	}
}

func validCheckpointTrigger(trigger CheckpointTrigger) bool {
	switch trigger {
	case CheckpointTriggerStarting, CheckpointTriggerReady, CheckpointTriggerExited, CheckpointTriggerRestartScheduled, CheckpointTriggerCrashLoop, CheckpointTriggerStopRequested:
		return true
	default:
		return false
	}
}

func trimRestartHistory(history []RestartRecord, limit int) []RestartRecord {
	if limit <= 0 || limit > 64 {
		limit = 64
	}
	if len(history) <= limit {
		return history
	}
	return append([]RestartRecord(nil), history[len(history)-limit:]...)
}

func checkpointObservedAt(snapshot Snapshot) time.Time {
	for _, candidate := range []time.Time{snapshot.ExitedAt, snapshot.NextRestartAt, snapshot.LastProbeAt, snapshot.LastHealthyAt, snapshot.StartedAt} {
		if !candidate.IsZero() {
			return candidate
		}
	}
	return time.Time{}
}
