CREATE TABLE mzfw_task_checkpoints (
    task_id TEXT NOT NULL,
    checkpoint_key TEXT NOT NULL,
    checkpoint_revision INTEGER NOT NULL,
    attempt_no INTEGER NOT NULL,
    output_cursor INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(task_id, checkpoint_key),
    UNIQUE(task_id, checkpoint_revision)
);

CREATE INDEX mzfw_task_checkpoints_latest_idx
ON mzfw_task_checkpoints(task_id, checkpoint_revision DESC);
