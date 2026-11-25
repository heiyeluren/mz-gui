CREATE TABLE mzfw_audit_events (
    audit_id TEXT PRIMARY KEY,
    occurred_at TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    capability TEXT,
    resource_type TEXT,
    resource_id TEXT,
    decision TEXT NOT NULL,
    reason_code TEXT,
    request_id TEXT,
    metadata_json TEXT NOT NULL
);

CREATE INDEX mzfw_audit_events_resource_occurred_at_idx
ON mzfw_audit_events(resource_type, resource_id, occurred_at DESC);
