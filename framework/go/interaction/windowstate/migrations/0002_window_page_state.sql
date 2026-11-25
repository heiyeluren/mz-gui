CREATE TABLE mzfw_window_page_state (
    window_id TEXT NOT NULL,
    route_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    payload BLOB NOT NULL,
    saved_at TEXT NOT NULL,
    PRIMARY KEY (window_id, route_id)
);
