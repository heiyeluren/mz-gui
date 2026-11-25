CREATE TABLE mzfw_window_state (
    window_id TEXT PRIMARY KEY,
    route_id TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL,
    payload BLOB NOT NULL,
    saved_at TEXT NOT NULL
);
