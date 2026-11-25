CREATE TABLE tasks (
    task_id TEXT PRIMARY KEY,
    task_type TEXT NOT NULL,
    state TEXT NOT NULL,
    priority INTEGER NOT NULL,
    payload_version INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    idempotency_key TEXT,
    workspace_id TEXT,
    required_capabilities_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    scheduled_at TEXT,
    started_at TEXT,
    finished_at TEXT,
    lease_owner TEXT,
    lease_expires_at TEXT,
    attempt_count INTEGER NOT NULL,
    max_attempts INTEGER NOT NULL,
    next_retry_at TEXT,
    cancel_requested_at TEXT,
    result_summary_json TEXT,
    error_code TEXT,
    revision INTEGER NOT NULL
);

CREATE UNIQUE INDEX tasks_idempotency_key_uq ON tasks(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX tasks_state_scheduled_at_idx ON tasks(state, scheduled_at);
CREATE INDEX tasks_next_retry_at_idx ON tasks(next_retry_at);
CREATE INDEX tasks_workspace_created_at_idx ON tasks(workspace_id, created_at);

CREATE TABLE task_attempts (
    attempt_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    attempt_no INTEGER NOT NULL,
    worker_id TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    outcome TEXT,
    error_code TEXT,
    error_summary TEXT,
    output_start_cursor INTEGER,
    output_end_cursor INTEGER,
    UNIQUE(task_id, attempt_no)
);

CREATE TABLE task_output (
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    cursor INTEGER NOT NULL,
    kind TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    payload BLOB NOT NULL,
    PRIMARY KEY(task_id, cursor)
);

CREATE TABLE rpc_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    task_id TEXT NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    result_revision INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE schedules (
    schedule_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL,
    kind TEXT NOT NULL,
    expression TEXT NOT NULL,
    timezone TEXT NOT NULL,
    misfire_policy TEXT NOT NULL,
    overlap_policy TEXT NOT NULL,
    jitter_ms INTEGER NOT NULL,
    next_run_at TEXT,
    last_run_at TEXT,
    payload_template TEXT NOT NULL,
    revision INTEGER NOT NULL,
    max_catch_up INTEGER NOT NULL DEFAULT 1,
    overlap_limit INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX schedules_next_run_at_idx ON schedules(next_run_at);
CREATE INDEX schedules_active_updated_at_idx ON schedules(deleted_at, updated_at DESC, schedule_id DESC);

CREATE TABLE schedule_occurrences (
    occurrence_id TEXT PRIMARY KEY,
    schedule_id TEXT NOT NULL REFERENCES schedules(schedule_id) ON DELETE CASCADE,
    planned_at TEXT NOT NULL,
    task_id TEXT REFERENCES tasks(task_id),
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    reason_code TEXT,
    UNIQUE(schedule_id, planned_at)
);

CREATE TABLE schedule_rpc_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    schedule_id TEXT NOT NULL REFERENCES schedules(schedule_id) ON DELETE CASCADE,
    occurrence_id TEXT REFERENCES schedule_occurrences(occurrence_id) ON DELETE SET NULL,
    result_revision INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX schedule_rpc_idempotency_schedule_created_at_idx ON schedule_rpc_idempotency(schedule_id, created_at);
