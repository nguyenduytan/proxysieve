CREATE TABLE traffic_aggregates_hour (
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

CREATE INDEX traffic_aggregates_hour_bucket ON traffic_aggregates_hour(bucket_start DESC);

CREATE TABLE traffic_aggregates_day (
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

CREATE INDEX traffic_aggregates_day_bucket ON traffic_aggregates_day(bucket_start DESC);

CREATE TABLE traffic_dirty_hours (
    bucket_start INTEGER PRIMARY KEY
) STRICT;

CREATE TABLE traffic_dirty_days (
    bucket_start INTEGER PRIMARY KEY
) STRICT;

INSERT INTO traffic_dirty_hours(bucket_start)
SELECT DISTINCT (bucket_start / 3600000000000) * 3600000000000
FROM traffic_aggregates_minute;

CREATE TRIGGER traffic_minute_insert_marks_hour AFTER INSERT ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start)
    VALUES ((NEW.bucket_start / 3600000000000) * 3600000000000);
END;

CREATE TRIGGER traffic_minute_update_marks_hour AFTER UPDATE ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start)
    VALUES ((OLD.bucket_start / 3600000000000) * 3600000000000);
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start)
    VALUES ((NEW.bucket_start / 3600000000000) * 3600000000000);
END;

CREATE TRIGGER traffic_minute_delete_marks_hour AFTER DELETE ON traffic_aggregates_minute BEGIN
    INSERT OR IGNORE INTO traffic_dirty_hours(bucket_start)
    VALUES ((OLD.bucket_start / 3600000000000) * 3600000000000);
END;

CREATE TRIGGER traffic_hour_insert_marks_day AFTER INSERT ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start)
    VALUES ((NEW.bucket_start / 86400000000000) * 86400000000000);
END;

CREATE TRIGGER traffic_hour_update_marks_day AFTER UPDATE ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start)
    VALUES ((OLD.bucket_start / 86400000000000) * 86400000000000);
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start)
    VALUES ((NEW.bucket_start / 86400000000000) * 86400000000000);
END;

CREATE TRIGGER traffic_hour_delete_marks_day AFTER DELETE ON traffic_aggregates_hour BEGIN
    INSERT OR IGNORE INTO traffic_dirty_days(bucket_start)
    VALUES ((OLD.bucket_start / 86400000000000) * 86400000000000);
END;
