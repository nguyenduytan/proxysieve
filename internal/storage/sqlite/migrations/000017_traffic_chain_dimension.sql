ALTER TABLE traffic_events ADD COLUMN chain_id TEXT;
CREATE INDEX traffic_events_chain_at ON traffic_events(chain_id, at DESC);

DROP TRIGGER traffic_minute_insert_marks_hour;
DROP TRIGGER traffic_minute_update_marks_hour;
DROP TRIGGER traffic_minute_delete_marks_hour;
DROP TRIGGER traffic_hour_insert_marks_day;
DROP TRIGGER traffic_hour_update_marks_day;
DROP TRIGGER traffic_hour_delete_marks_day;
DROP INDEX traffic_aggregates_minute_bucket;
DROP INDEX traffic_aggregates_hour_bucket;
DROP INDEX traffic_aggregates_day_bucket;
DROP INDEX traffic_cost_minute_bucket;
DROP INDEX traffic_cost_hour_bucket;
DROP INDEX traffic_cost_day_bucket;

ALTER TABLE traffic_aggregates_minute RENAME TO traffic_aggregates_minute_v16;
ALTER TABLE traffic_aggregates_hour RENAME TO traffic_aggregates_hour_v16;
ALTER TABLE traffic_aggregates_day RENAME TO traffic_aggregates_day_v16;
ALTER TABLE traffic_cost_aggregates_minute RENAME TO traffic_cost_aggregates_minute_v16;
ALTER TABLE traffic_cost_aggregates_hour RENAME TO traffic_cost_aggregates_hour_v16;
ALTER TABLE traffic_cost_aggregates_day RENAME TO traffic_cost_aggregates_day_v16;

CREATE TABLE traffic_aggregates_minute (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    request_count INTEGER NOT NULL, client_upload INTEGER NOT NULL, client_download INTEGER NOT NULL,
    upstream_upload INTEGER NOT NULL, upstream_download INTEGER NOT NULL, direct_bytes INTEGER NOT NULL,
    cache_served INTEGER NOT NULL, health_check INTEGER NOT NULL, estimated_avoided INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id)
) STRICT;
CREATE TABLE traffic_aggregates_hour (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    request_count INTEGER NOT NULL, client_upload INTEGER NOT NULL, client_download INTEGER NOT NULL,
    upstream_upload INTEGER NOT NULL, upstream_download INTEGER NOT NULL, direct_bytes INTEGER NOT NULL,
    cache_served INTEGER NOT NULL, health_check INTEGER NOT NULL, estimated_avoided INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id)
) STRICT;
CREATE TABLE traffic_aggregates_day (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    request_count INTEGER NOT NULL, client_upload INTEGER NOT NULL, client_download INTEGER NOT NULL,
    upstream_upload INTEGER NOT NULL, upstream_download INTEGER NOT NULL, direct_bytes INTEGER NOT NULL,
    cache_served INTEGER NOT NULL, health_check INTEGER NOT NULL, estimated_avoided INTEGER NOT NULL,
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id)
) STRICT;

INSERT INTO traffic_aggregates_minute SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided FROM traffic_aggregates_minute_v16;
INSERT INTO traffic_aggregates_hour SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided FROM traffic_aggregates_hour_v16;
INSERT INTO traffic_aggregates_day SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided FROM traffic_aggregates_day_v16;

CREATE TABLE traffic_cost_aggregates_minute (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    currency TEXT NOT NULL, configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id, currency)
) STRICT;
CREATE TABLE traffic_cost_aggregates_hour (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    currency TEXT NOT NULL, configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id, currency)
) STRICT;
CREATE TABLE traffic_cost_aggregates_day (
    bucket_start INTEGER NOT NULL, action TEXT NOT NULL, protocol TEXT NOT NULL,
    client_id TEXT NOT NULL, pool_id TEXT NOT NULL, proxy_id TEXT NOT NULL, chain_id TEXT NOT NULL,
    currency TEXT NOT NULL, configured_cost_micros INTEGER NOT NULL CHECK (configured_cost_micros >= 0),
    priced_upstream_upload INTEGER NOT NULL CHECK (priced_upstream_upload >= 0),
    priced_upstream_download INTEGER NOT NULL CHECK (priced_upstream_download >= 0),
    PRIMARY KEY (bucket_start, action, protocol, client_id, pool_id, proxy_id, chain_id, currency)
) STRICT;

INSERT INTO traffic_cost_aggregates_minute SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download FROM traffic_cost_aggregates_minute_v16;
INSERT INTO traffic_cost_aggregates_hour SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download FROM traffic_cost_aggregates_hour_v16;
INSERT INTO traffic_cost_aggregates_day SELECT bucket_start,action,protocol,client_id,pool_id,proxy_id,'',currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download FROM traffic_cost_aggregates_day_v16;

DROP TABLE traffic_aggregates_minute_v16;
DROP TABLE traffic_aggregates_hour_v16;
DROP TABLE traffic_aggregates_day_v16;
DROP TABLE traffic_cost_aggregates_minute_v16;
DROP TABLE traffic_cost_aggregates_hour_v16;
DROP TABLE traffic_cost_aggregates_day_v16;

CREATE INDEX traffic_aggregates_minute_bucket ON traffic_aggregates_minute(bucket_start DESC);
CREATE INDEX traffic_aggregates_hour_bucket ON traffic_aggregates_hour(bucket_start DESC);
CREATE INDEX traffic_aggregates_day_bucket ON traffic_aggregates_day(bucket_start DESC);
CREATE INDEX traffic_cost_minute_bucket ON traffic_cost_aggregates_minute(bucket_start DESC);
CREATE INDEX traffic_cost_hour_bucket ON traffic_cost_aggregates_hour(bucket_start DESC);
CREATE INDEX traffic_cost_day_bucket ON traffic_cost_aggregates_day(bucket_start DESC);

CREATE TRIGGER traffic_minute_insert_marks_hour AFTER INSERT ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start) VALUES ((NEW.bucket_start / 3600000000000) * 3600000000000);
END;
CREATE TRIGGER traffic_minute_update_marks_hour AFTER UPDATE ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start) VALUES ((OLD.bucket_start / 3600000000000) * 3600000000000);
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start) VALUES ((NEW.bucket_start / 3600000000000) * 3600000000000);
END;
CREATE TRIGGER traffic_minute_delete_marks_hour AFTER DELETE ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start) VALUES ((OLD.bucket_start / 3600000000000) * 3600000000000);
END;
CREATE TRIGGER traffic_hour_insert_marks_day AFTER INSERT ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start) VALUES ((NEW.bucket_start / 86400000000000) * 86400000000000);
END;
CREATE TRIGGER traffic_hour_update_marks_day AFTER UPDATE ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start) VALUES ((OLD.bucket_start / 86400000000000) * 86400000000000);
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start) VALUES ((NEW.bucket_start / 86400000000000) * 86400000000000);
END;
CREATE TRIGGER traffic_hour_delete_marks_day AFTER DELETE ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start) VALUES ((OLD.bucket_start / 86400000000000) * 86400000000000);
END;
