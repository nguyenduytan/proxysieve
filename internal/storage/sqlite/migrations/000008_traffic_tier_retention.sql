-- Monotonic per-tier watermarks make each aggregate table an authoritative,
-- non-overlapping slice of the analytics timeline.
CREATE TABLE traffic_aggregate_retention (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    minute_before_ns INTEGER NOT NULL CHECK (minute_before_ns >= 0),
    hour_before_ns INTEGER NOT NULL CHECK (hour_before_ns >= 0),
    day_before_ns INTEGER NOT NULL CHECK (day_before_ns >= 0),
    CHECK (day_before_ns <= hour_before_ns),
    CHECK (hour_before_ns <= minute_before_ns)
) STRICT;

INSERT INTO traffic_aggregate_retention(
    singleton,
    minute_before_ns,
    hour_before_ns,
    day_before_ns
) VALUES (1, 0, 0, 0);
