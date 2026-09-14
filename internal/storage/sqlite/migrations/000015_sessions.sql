CREATE TABLE sessions (
    id TEXT PRIMARY KEY NOT NULL,
    document TEXT NOT NULL CHECK (length(document) <= 1048576 AND json_valid(document)),
    CHECK (json_extract(document, '$.id') = id)
) STRICT;

CREATE UNIQUE INDEX sessions_active_key
    ON sessions(json_extract(document, '$.key_hash'))
    WHERE json_extract(document, '$.status') = 'active';

CREATE INDEX sessions_created_at
    ON sessions(json_extract(document, '$.created_at') DESC, id);
