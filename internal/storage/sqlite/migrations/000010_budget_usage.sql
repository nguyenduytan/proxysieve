CREATE TABLE budget_usage (
    budget_id TEXT PRIMARY KEY NOT NULL,
    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK (reserved_bytes >= 0)
) STRICT;
