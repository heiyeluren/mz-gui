CREATE TABLE mzfw_capability_grants (
    grant_id TEXT PRIMARY KEY,
    subject TEXT NOT NULL,
    capability TEXT NOT NULL,
    scope_json TEXT NOT NULL,
    risk_level TEXT NOT NULL,
    granted_at TEXT NOT NULL,
    expires_at TEXT,
    revoked_at TEXT,
    grant_source TEXT NOT NULL,
    revision INTEGER NOT NULL
);

CREATE INDEX mzfw_capability_grants_subject_capability_active_idx
ON mzfw_capability_grants(subject, capability, revoked_at, expires_at);
