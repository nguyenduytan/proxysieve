CREATE TABLE webhook_configs (
    id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 65536)
) STRICT;

CREATE TABLE alert_rules (
    id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 65536)
) STRICT;

CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES webhook_configs(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL,
    attempted_at INTEGER NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    success INTEGER NOT NULL CHECK (success IN (0, 1)),
    status_code INTEGER NOT NULL CHECK (status_code BETWEEN 0 AND 999),
    error_code TEXT NOT NULL CHECK (length(error_code) <= 64),
    duration_ns INTEGER NOT NULL CHECK (duration_ns >= 0),
    UNIQUE(webhook_id, event_id, attempt)
) STRICT;

CREATE INDEX webhook_deliveries_recent ON webhook_deliveries(webhook_id, attempted_at DESC, id DESC);
