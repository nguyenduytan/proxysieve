CREATE TABLE traffic_events (
    id INTEGER PRIMARY KEY,
    at INTEGER NOT NULL,
    request_id TEXT NOT NULL,
    connection_id TEXT NOT NULL,
    client_id TEXT,
    pool_id TEXT,
    proxy_id TEXT,
    host TEXT NOT NULL,
    protocol TEXT NOT NULL,
    action TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    client_upload INTEGER NOT NULL,
    client_download INTEGER NOT NULL,
    upstream_upload INTEGER NOT NULL,
    upstream_download INTEGER NOT NULL,
    direct_bytes INTEGER NOT NULL,
    cache_served INTEGER NOT NULL,
    health_check INTEGER NOT NULL,
    estimated_avoided INTEGER NOT NULL,
    CHECK (client_upload >= 0 AND client_download >= 0 AND upstream_upload >= 0 AND upstream_download >= 0),
    CHECK (direct_bytes >= 0 AND cache_served >= 0 AND health_check >= 0 AND estimated_avoided >= 0)
) STRICT;

CREATE INDEX traffic_events_at ON traffic_events(at DESC, id DESC);
CREATE INDEX traffic_events_client_at ON traffic_events(client_id, at DESC);
CREATE INDEX traffic_events_proxy_at ON traffic_events(proxy_id, at DESC);

CREATE TABLE traffic_aggregates_minute (
    bucket_start INTEGER NOT NULL,
    action TEXT NOT NULL,
    protocol TEXT NOT NULL,
    client_id TEXT NOT NULL,
    pool_id TEXT NOT NULL,
    proxy_id TEXT NOT NULL,
    request_count INTEGER NOT NULL,
    client_upload INTEGER NOT NULL,
    client_download INTEGER NOT NULL,
    upstream_upload INTEGER NOT NULL,
    upstream_download INTEGER NOT NULL,
    direct_bytes INTEGER NOT NULL,
    cache_served INTEGER NOT NULL,
    health_check INTEGER NOT NULL,
    estimated_avoided INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id)
) STRICT;

CREATE INDEX traffic_aggregates_minute_bucket ON traffic_aggregates_minute(bucket_start DESC);
