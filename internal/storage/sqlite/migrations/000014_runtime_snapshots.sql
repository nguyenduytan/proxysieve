CREATE TABLE runtime_snapshots (
    revision INTEGER PRIMARY KEY,
    activated_at TEXT NOT NULL,
    activated_by TEXT NOT NULL,
    source_revision INTEGER NOT NULL DEFAULT 0 CHECK (source_revision >= 0),
    document BLOB NOT NULL,
    is_current INTEGER NOT NULL CHECK (is_current IN (0, 1))
) STRICT;

CREATE UNIQUE INDEX runtime_snapshots_one_current
    ON runtime_snapshots(is_current)
    WHERE is_current = 1;
