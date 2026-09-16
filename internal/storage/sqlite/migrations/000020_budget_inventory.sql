CREATE TABLE budget_configs (
    id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 65536)
) STRICT;

CREATE TABLE budget_inventory_state (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    initialized INTEGER NOT NULL CHECK (initialized IN (0, 1))
) STRICT;

INSERT INTO budget_inventory_state(singleton, initialized) VALUES (1, 0);
