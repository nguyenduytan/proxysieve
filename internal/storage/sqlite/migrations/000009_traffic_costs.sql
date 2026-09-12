ALTER TABLE traffic_events ADD COLUMN cost_currency TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_events ADD COLUMN cost_micros INTEGER NOT NULL DEFAULT 0 CHECK (cost_micros >= 0);
ALTER TABLE traffic_events ADD COLUMN rate_price_micros INTEGER NOT NULL DEFAULT 0 CHECK (rate_price_micros >= 0);
ALTER TABLE traffic_events ADD COLUMN rate_unit_bytes INTEGER NOT NULL DEFAULT 0 CHECK (rate_unit_bytes >= 0);
ALTER TABLE traffic_events ADD COLUMN rate_download_only INTEGER NOT NULL DEFAULT 0 CHECK (rate_download_only IN (0, 1));
ALTER TABLE traffic_events ADD COLUMN rate_effective_at INTEGER NOT NULL DEFAULT 0 CHECK (rate_effective_at >= 0);

CREATE TABLE traffic_cost_aggregates_minute (
    bucket_start INTEGER NOT NULL,
    action TEXT NOT NULL,
    protocol TEXT NOT NULL,
    client_id TEXT NOT NULL,
    pool_id TEXT NOT NULL,
    proxy_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, currency)
) STRICT;

CREATE TABLE traffic_cost_aggregates_hour (
    bucket_start INTEGER NOT NULL,
    action TEXT NOT NULL,
    protocol TEXT NOT NULL,
    client_id TEXT NOT NULL,
    pool_id TEXT NOT NULL,
    proxy_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, currency)
) STRICT;

CREATE TABLE traffic_cost_aggregates_day (
    bucket_start INTEGER NOT NULL,
    action TEXT NOT NULL,
    protocol TEXT NOT NULL,
    client_id TEXT NOT NULL,
    pool_id TEXT NOT NULL,
    proxy_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, currency)
) STRICT;

CREATE INDEX traffic_cost_minute_bucket ON traffic_cost_aggregates_minute(bucket_start DESC);
CREATE INDEX traffic_cost_hour_bucket ON traffic_cost_aggregates_hour(bucket_start DESC);
CREATE INDEX traffic_cost_day_bucket ON traffic_cost_aggregates_day(bucket_start DESC);
