CREATE TABLE audit_log (
    id TEXT PRIMARY KEY NOT NULL,
    at INTEGER NOT NULL,
    actor_id TEXT,
    action TEXT NOT NULL CHECK (length(action) BETWEEN 2 AND 128),
    target_type TEXT NOT NULL CHECK (length(target_type) BETWEEN 2 AND 128),
    target_id TEXT,
    request_id TEXT
) STRICT;

CREATE INDEX audit_log_at ON audit_log(at DESC, id DESC);
CREATE INDEX audit_log_actor ON audit_log(actor_id, at DESC);
