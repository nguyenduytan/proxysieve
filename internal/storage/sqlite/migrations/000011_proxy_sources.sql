CREATE TABLE proxy_sources (
    id TEXT PRIMARY KEY NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 1048576 AND json_valid(document)),
    CHECK (json_extract(document, '$.id') = id)
) STRICT;

CREATE INDEX proxy_sources_type ON proxy_sources(json_extract(document, '$.type'));
