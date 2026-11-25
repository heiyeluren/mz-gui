CREATE TABLE mzfw_schedules (
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

CREATE INDEX mzfw_schedules_next_run_at_idx ON mzfw_schedules(next_run_at);
CREATE INDEX mzfw_schedules_active_updated_at_idx
ON mzfw_schedules(deleted_at, updated_at DESC, schedule_id DESC);

CREATE TABLE mzfw_schedule_occurrences (
    occurrence_id TEXT PRIMARY KEY,
    schedule_id TEXT NOT NULL REFERENCES mzfw_schedules(schedule_id) ON DELETE CASCADE,
    planned_at TEXT NOT NULL,
    task_id TEXT,
    state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    reason_code TEXT,
    UNIQUE(schedule_id, planned_at)
);

CREATE TABLE mzfw_schedule_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    schedule_id TEXT NOT NULL REFERENCES mzfw_schedules(schedule_id) ON DELETE CASCADE,
    occurrence_id TEXT REFERENCES mzfw_schedule_occurrences(occurrence_id) ON DELETE SET NULL,
    result_revision INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX mzfw_schedule_idempotency_schedule_created_at_idx
ON mzfw_schedule_idempotency(schedule_id, created_at);
