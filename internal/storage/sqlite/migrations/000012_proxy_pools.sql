CREATE TABLE proxy_pools (
    id TEXT PRIMARY KEY NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 1048576 AND json_valid(document)),
    CHECK (json_extract(document, '$.id') = id)
) STRICT;

CREATE INDEX proxy_pools_strategy ON proxy_pools(json_extract(document, '$.strategy'));
