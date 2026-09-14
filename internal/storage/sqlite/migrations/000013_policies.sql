CREATE TABLE policies (
    id TEXT PRIMARY KEY NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 1048576 AND json_valid(document)),
    CHECK (json_extract(document, '$.id') = id),
    CHECK (json_extract(document, '$.version') = 1)
) STRICT;
