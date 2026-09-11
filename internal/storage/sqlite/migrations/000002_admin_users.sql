CREATE TABLE admin_users (
    id TEXT PRIMARY KEY NOT NULL,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(username) BETWEEN 3 AND 64),
    password_hash TEXT NOT NULL CHECK (length(password_hash) BETWEEN 32 AND 1024),
    role TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at INTEGER NOT NULL,
    last_login_at INTEGER
) STRICT;

CREATE INDEX admin_users_enabled ON admin_users(enabled);
