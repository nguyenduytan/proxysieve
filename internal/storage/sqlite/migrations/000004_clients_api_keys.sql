CREATE TABLE clients (
    id TEXT PRIMARY KEY NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    document TEXT NOT NULL CHECK (length(document) <= 262144 AND json_valid(document)),
    CHECK (json_extract(document, '$.id') = id)
) STRICT;

CREATE TABLE api_keys (
    id TEXT PRIMARY KEY NOT NULL,
    client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    prefix TEXT NOT NULL CHECK (length(prefix) BETWEEN 8 AND 32),
    token_hash BLOB NOT NULL CHECK (length(token_hash) = 32),
    created_at INTEGER NOT NULL,
    revoked_at INTEGER,
    UNIQUE(token_hash)
) STRICT;

CREATE INDEX api_keys_client ON api_keys(client_id, created_at DESC);
