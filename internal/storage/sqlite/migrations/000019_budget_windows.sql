ALTER TABLE budget_usage RENAME TO budget_usage_v18;

CREATE TABLE budget_usage (
    budget_id TEXT NOT NULL,
    window_start INTEGER NOT NULL,
    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK (reserved_bytes >= 0),
    PRIMARY KEY (budget_id, window_start)
) STRICT;

INSERT INTO budget_usage(budget_id,window_start,used_bytes,reserved_bytes)
SELECT budget_id,0,used_bytes,reserved_bytes FROM budget_usage_v18;

DROP TABLE budget_usage_v18;
