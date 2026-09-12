-- Never rebuild a bucket after its source events have been retired.
CREATE TABLE traffic_retention (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    before_ns INTEGER NOT NULL CHECK (before_ns >= 0)
) STRICT;
INSERT INTO traffic_retention(singleton, before_ns) VALUES (1, 0);

-- Track changed buckets transactionally, including rows already present at
-- upgrade. Catch-up work does not rebuild the entire retention window per tick.
CREATE TABLE traffic_dirty_minutes (
    bucket_start INTEGER PRIMARY KEY
) STRICT;
INSERT INTO traffic_dirty_minutes(bucket_start)
SELECT DISTINCT (at / 60000000000) * 60000000000 FROM traffic_events;

CREATE TRIGGER traffic_mark_dirty AFTER INSERT ON traffic_events BEGIN
    INSERT OR IGNORE INTO traffic_dirty_minutes(bucket_start)
    VALUES ((NEW.at / 60000000000) * 60000000000);
END;
