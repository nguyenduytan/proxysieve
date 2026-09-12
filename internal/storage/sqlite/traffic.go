package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
)

const minuteNanos = int64(time.Minute)
const hourNanos = int64(time.Hour)
const dayNanos = int64(24 * time.Hour)

type TrafficPage struct {
	Before time.Time
	Limit  int
}

type TrafficQuery struct {
	From        time.Time
	Until       time.Time
	Granularity time.Duration
	ClientID    model.ID
	PoolID      model.ID
	ProxyID     model.ID
	Action      string
	Protocol    string
}

type TrafficRetention struct {
	RawBefore    time.Time
	MinuteBefore time.Time
	HourBefore   time.Time
	DayBefore    time.Time
}

type TrafficRetentionResult struct {
	RawEvents int64
	Minutes   int64
	Hours     int64
	Days      int64
}

func (q TrafficQuery) valid(summary bool) bool {
	if !validTrafficTime(q.From) || !validTrafficTime(q.Until) || !q.From.Before(q.Until) ||
		!q.From.Equal(q.From.Truncate(time.Minute)) || !q.Until.Equal(q.Until.Truncate(time.Minute)) ||
		q.Until.Sub(q.From) > 10*365*24*time.Hour {
		return false
	}
	for _, id := range []model.ID{q.ClientID, q.PoolID, q.ProxyID} {
		if id != "" && !id.Valid() {
			return false
		}
	}
	if len(q.Action) > 32 || len(q.Protocol) > 32 || strings.ContainsAny(q.Action+q.Protocol, "\r\n") {
		return false
	}
	if summary {
		return q.Granularity == 0
	}
	if q.Granularity != time.Minute && q.Granularity != time.Hour && q.Granularity != 24*time.Hour {
		return false
	}
	return q.From.Equal(q.From.Truncate(q.Granularity)) &&
		q.Until.Equal(q.Until.Truncate(q.Granularity)) &&
		q.Until.Sub(q.From)/q.Granularity <= 2000
}

func (q TrafficQuery) ValidSummary() bool { return q.valid(true) }
func (q TrafficQuery) ValidSeries() bool  { return q.valid(false) }

func (p TrafficPage) Valid() bool { return p.Limit >= 1 && p.Limit <= 1000 }
func (s *Store) RecordTraffic(ctx context.Context, event traffic.Event) error {
	return s.RecordTrafficBatch(ctx, []traffic.Event{event})
}

func (s *Store) RecordTrafficBatch(ctx context.Context, events []traffic.Event) error {
	if len(events) == 0 || len(events) > 10_000 {
		return traffic.ErrInvalidEvent
	}
	for _, event := range events {
		if event.Validate() != nil {
			return traffic.ErrInvalidEvent
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO traffic_events(at,request_id,connection_id,client_id,pool_id,proxy_id,host,protocol,action,status_code,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided,cost_currency,cost_micros,rate_price_micros,rate_unit_bytes,rate_download_only,rate_effective_at)
	SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE ? >= (SELECT before_ns FROM traffic_retention WHERE singleton=1)`)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = statement.Close() }()
	for _, event := range events {
		at := event.At.UTC().UnixNano()
		currency, costMicros, priceMicros, unitBytes, downloadOnly, effectiveAt := trafficCostValues(event.ConfiguredCost)
		result, err := statement.ExecContext(ctx, at, string(event.RequestID), string(event.ConnectionID), nullableID(event.ClientID), nullableID(event.PoolID), nullableID(event.ProxyID), event.Host, event.Protocol, event.Action, event.StatusCode, int64(event.ClientUpload), int64(event.ClientDownload), int64(event.UpstreamUpload), int64(event.UpstreamDownload), int64(event.Direct), int64(event.CacheServed), int64(event.HealthCheck), int64(event.EstimatedAvoided), currency, costMicros, priceMicros, unitBytes, downloadOnly, effectiveAt, at)
		if err != nil {
			return safeError(ctx, err)
		}
		if n, err := result.RowsAffected(); err != nil {
			return safeError(ctx, err)
		} else if n != 1 {
			return traffic.ErrInvalidEvent
		}
	}
	return safeError(ctx, tx.Commit())
}
func (s *Store) ListTraffic(ctx context.Context, page TrafficPage) ([]traffic.Event, error) {
	if !page.Valid() {
		return nil, store.ErrInvalid
	}
	args := []any{}
	query := `SELECT at,request_id,connection_id,client_id,pool_id,proxy_id,host,protocol,action,status_code,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided,cost_currency,cost_micros,rate_price_micros,rate_unit_bytes,rate_download_only,rate_effective_at FROM traffic_events`
	if !page.Before.IsZero() {
		query += ` WHERE at < ?`
		args = append(args, page.Before.UTC().UnixNano())
	}
	query += ` ORDER BY at DESC,id DESC LIMIT ?`
	args = append(args, page.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]traffic.Event, 0, page.Limit)
	for rows.Next() {
		event, err := scanTraffic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return out, nil
}
func (s *Store) RollupMinute(ctx context.Context, from, until time.Time) error {
	if !validTrafficTime(from) || !validTrafficTime(until) || !from.Before(until) || !from.Equal(from.Truncate(time.Minute)) || !until.Equal(until.Truncate(time.Minute)) {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = rollupMinute(ctx, tx, from.UnixNano(), until.UnixNano()); err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
}

func (s *Store) RollupHour(ctx context.Context, from, until time.Time) error {
	if !validRollupWindow(from, until, time.Hour) {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = rollupHour(ctx, tx, from.UTC().UnixNano(), until.UTC().UnixNano()); err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
}

func rollupHour(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_aggregates_hour WHERE bucket_start IN
	(SELECT bucket_start FROM traffic_dirty_hours WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_aggregates_hour(bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided)
SELECT (minute.bucket_start / ?) * ?,minute.action,minute.protocol,minute.client_id,minute.pool_id,minute.proxy_id,sum(minute.request_count),sum(minute.client_upload),sum(minute.client_download),sum(minute.upstream_upload),sum(minute.upstream_download),sum(minute.direct_bytes),sum(minute.cache_served),sum(minute.health_check),sum(minute.estimated_avoided)
FROM traffic_dirty_hours AS dirty JOIN traffic_aggregates_minute AS minute
ON minute.bucket_start>=dirty.bucket_start AND minute.bucket_start<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? GROUP BY 1,2,3,4,5,6`, hourNanos, hourNanos, hourNanos, start, end)
	if err != nil {
		return err
	}
	if err = rollupCostHour(ctx, tx, start, end); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_dirty_hours WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	return nil
}

func compactHour(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_aggregates_hour WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_aggregates_hour(bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided)
SELECT (bucket_start / ?) * ?,action,protocol,client_id,pool_id,proxy_id,sum(request_count),sum(client_upload),sum(client_download),sum(upstream_upload),sum(upstream_download),sum(direct_bytes),sum(cache_served),sum(health_check),sum(estimated_avoided)
FROM traffic_aggregates_minute WHERE bucket_start>=? AND bucket_start<? GROUP BY 1,2,3,4,5,6`, hourNanos, hourNanos, start, end)
	if err != nil {
		return err
	}
	return compactCostHour(ctx, tx, start, end)
}

func (s *Store) RollupDay(ctx context.Context, from, until time.Time) error {
	if !validRollupWindow(from, until, 24*time.Hour) {
		return store.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = rollupDay(ctx, tx, from.UTC().UnixNano(), until.UTC().UnixNano()); err != nil {
		return safeError(ctx, err)
	}
	return safeError(ctx, tx.Commit())
}

func rollupDay(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_aggregates_day WHERE bucket_start IN
	(SELECT bucket_start FROM traffic_dirty_days WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_aggregates_day(bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided)
SELECT (hour.bucket_start / ?) * ?,hour.action,hour.protocol,hour.client_id,hour.pool_id,hour.proxy_id,sum(hour.request_count),sum(hour.client_upload),sum(hour.client_download),sum(hour.upstream_upload),sum(hour.upstream_download),sum(hour.direct_bytes),sum(hour.cache_served),sum(hour.health_check),sum(hour.estimated_avoided)
FROM traffic_dirty_days AS dirty JOIN traffic_aggregates_hour AS hour
ON hour.bucket_start>=dirty.bucket_start AND hour.bucket_start<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? GROUP BY 1,2,3,4,5,6`, dayNanos, dayNanos, dayNanos, start, end)
	if err != nil {
		return err
	}
	if err = rollupCostDay(ctx, tx, start, end); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_dirty_days WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	return nil
}

func compactDay(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_aggregates_day WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_aggregates_day(bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided)
SELECT (bucket_start / ?) * ?,action,protocol,client_id,pool_id,proxy_id,sum(request_count),sum(client_upload),sum(client_download),sum(upstream_upload),sum(upstream_download),sum(direct_bytes),sum(cache_served),sum(health_check),sum(estimated_avoided)
FROM traffic_aggregates_hour WHERE bucket_start>=? AND bucket_start<? GROUP BY 1,2,3,4,5,6`, dayNanos, dayNanos, start, end)
	if err != nil {
		return err
	}
	return compactCostDay(ctx, tx, start, end)
}

func validRollupWindow(from, until time.Time, width time.Duration) bool {
	return validTrafficTime(from) && validTrafficTime(until) && from.Before(until) &&
		from.Equal(from.Truncate(width)) && until.Equal(until.Truncate(width))
}

// Only complete, non-retired buckets can be rebuilt. Retention and rebuilding
// share one transaction so failures cannot delete the only copy of usage.
func rollupMinute(ctx context.Context, tx *sql.Tx, start, end int64) error {
	var retired int64
	if err := tx.QueryRowContext(ctx, "SELECT before_ns FROM traffic_retention WHERE singleton=1").Scan(&retired); err != nil {
		return err
	}
	start = max(start, retired)
	if start >= end {
		return nil
	}
	var err error
	if _, err = tx.ExecContext(ctx, `DELETE FROM traffic_aggregates_minute WHERE bucket_start IN
	(SELECT bucket_start FROM traffic_dirty_minutes WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return safeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO traffic_aggregates_minute(bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided)
SELECT (at / ?) * ?,action,protocol,coalesce(client_id,''),coalesce(pool_id,''),coalesce(proxy_id,''),count(*),sum(client_upload),sum(client_download),sum(upstream_upload),sum(upstream_download),sum(direct_bytes),sum(cache_served),sum(health_check),sum(estimated_avoided)
FROM traffic_dirty_minutes AS dirty JOIN traffic_events AS events
ON events.at>=dirty.bucket_start AND events.at<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? GROUP BY 1,2,3,4,5,6`, minuteNanos, minuteNanos, minuteNanos, start, end)
	if err != nil {
		return safeError(ctx, err)
	}
	if err = rollupCostMinute(ctx, tx, start, end); err != nil {
		return safeError(ctx, err)
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM traffic_dirty_minutes WHERE bucket_start>=? AND bucket_start<?", start, end)
	return err
}

func rollupCostMinute(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_cost_aggregates_minute WHERE bucket_start IN
(SELECT bucket_start FROM traffic_dirty_minutes WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_cost_aggregates_minute(bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download)
SELECT (at / ?) * ?,action,protocol,coalesce(client_id,''),coalesce(pool_id,''),coalesce(proxy_id,''),cost_currency,sum(cost_micros),sum(upstream_upload),sum(upstream_download)
FROM traffic_dirty_minutes AS dirty JOIN traffic_events AS events
ON events.at>=dirty.bucket_start AND events.at<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? AND cost_currency<>'' GROUP BY 1,2,3,4,5,6,7`, minuteNanos, minuteNanos, minuteNanos, start, end)
	return err
}

func rollupCostHour(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_cost_aggregates_hour WHERE bucket_start IN
(SELECT bucket_start FROM traffic_dirty_hours WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_cost_aggregates_hour(bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download)
SELECT (minute.bucket_start / ?) * ?,minute.action,minute.protocol,minute.client_id,minute.pool_id,minute.proxy_id,minute.currency,sum(minute.configured_cost_micros),sum(minute.priced_upstream_upload),sum(minute.priced_upstream_download)
FROM traffic_dirty_hours AS dirty JOIN traffic_cost_aggregates_minute AS minute
ON minute.bucket_start>=dirty.bucket_start AND minute.bucket_start<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? GROUP BY 1,2,3,4,5,6,7`, hourNanos, hourNanos, hourNanos, start, end)
	return err
}

func compactCostHour(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_cost_aggregates_hour WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_cost_aggregates_hour(bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download)
SELECT (bucket_start / ?) * ?,action,protocol,client_id,pool_id,proxy_id,currency,sum(configured_cost_micros),sum(priced_upstream_upload),sum(priced_upstream_download)
FROM traffic_cost_aggregates_minute WHERE bucket_start>=? AND bucket_start<? GROUP BY 1,2,3,4,5,6,7`, hourNanos, hourNanos, start, end)
	return err
}

func rollupCostDay(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_cost_aggregates_day WHERE bucket_start IN
(SELECT bucket_start FROM traffic_dirty_days WHERE bucket_start>=? AND bucket_start<?)`, start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_cost_aggregates_day(bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download)
SELECT (hour.bucket_start / ?) * ?,hour.action,hour.protocol,hour.client_id,hour.pool_id,hour.proxy_id,hour.currency,sum(hour.configured_cost_micros),sum(hour.priced_upstream_upload),sum(hour.priced_upstream_download)
FROM traffic_dirty_days AS dirty JOIN traffic_cost_aggregates_hour AS hour
ON hour.bucket_start>=dirty.bucket_start AND hour.bucket_start<dirty.bucket_start+?
WHERE dirty.bucket_start>=? AND dirty.bucket_start<? GROUP BY 1,2,3,4,5,6,7`, dayNanos, dayNanos, dayNanos, start, end)
	return err
}

func compactCostDay(ctx context.Context, tx *sql.Tx, start, end int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM traffic_cost_aggregates_day WHERE bucket_start>=? AND bucket_start<?", start, end); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO traffic_cost_aggregates_day(bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download)
SELECT (bucket_start / ?) * ?,action,protocol,client_id,pool_id,proxy_id,currency,sum(configured_cost_micros),sum(priced_upstream_upload),sum(priced_upstream_download)
FROM traffic_cost_aggregates_hour WHERE bucket_start>=? AND bucket_start<? GROUP BY 1,2,3,4,5,6,7`, dayNanos, dayNanos, start, end)
	return err
}
func (s *Store) RetainTraffic(ctx context.Context, before time.Time) (int64, error) {
	if !validTrafficTime(before) {
		return 0, store.ErrInvalid
	}
	// Keep the partial cutoff minute intact (less than one extra minute).
	end := before.UTC().Truncate(time.Minute).UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = rollupMinute(ctx, tx, 0, end); err != nil {
		return 0, safeError(ctx, err)
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM traffic_events WHERE at < ?", end)
	if err != nil {
		return 0, safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE traffic_retention SET before_ns=max(before_ns,?) WHERE singleton=1", end); err != nil {
		return 0, safeError(ctx, err)
	}
	if err = tx.Commit(); err != nil {
		return 0, safeError(ctx, err)
	}
	return count, nil
}

// RetainTrafficTiers compacts traffic into non-overlapping raw, minute, hour,
// and day ranges in one transaction. Each watermark is monotonic and the
// coarser tiers can never advance beyond the finer tier that feeds them.
func (s *Store) RetainTrafficTiers(ctx context.Context, retention TrafficRetention) (TrafficRetentionResult, error) {
	if !validTrafficTime(retention.RawBefore) || !validTrafficTime(retention.MinuteBefore) ||
		!validTrafficTime(retention.HourBefore) || !validTrafficTime(retention.DayBefore) {
		return TrafficRetentionResult{}, store.ErrInvalid
	}
	raw := retention.RawBefore.UTC().Truncate(time.Minute)
	minute := retention.MinuteBefore.UTC().Truncate(time.Hour)
	hour := retention.HourBefore.UTC().Truncate(24 * time.Hour)
	day := retention.DayBefore.UTC().Truncate(24 * time.Hour)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	defer func() { _ = tx.Rollback() }()
	var oldRaw, oldMinute, oldHour, oldDay int64
	if err = tx.QueryRowContext(ctx, "SELECT before_ns FROM traffic_retention WHERE singleton=1").Scan(&oldRaw); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if err = tx.QueryRowContext(ctx, "SELECT minute_before_ns,hour_before_ns,day_before_ns FROM traffic_aggregate_retention WHERE singleton=1").Scan(&oldMinute, &oldHour, &oldDay); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	// Requested cutoffs are lower bounds; never move a watermark backwards.
	rawNS := max(oldRaw, raw.UnixNano())
	minuteNS := min(max(oldMinute, minute.UnixNano()), rawNS)
	hourNS := min(max(oldHour, hour.UnixNano()), minuteNS)
	dayNS := min(max(oldDay, day.UnixNano()), hourNS)
	result := TrafficRetentionResult{}
	if rawNS > oldRaw {
		if err = rollupMinute(ctx, tx, oldRaw, rawNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
		res, e := tx.ExecContext(ctx, "DELETE FROM traffic_events WHERE at < ?", rawNS)
		if e != nil {
			return TrafficRetentionResult{}, safeError(ctx, e)
		}
		result.RawEvents, err = res.RowsAffected()
		if err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE traffic_retention SET before_ns=? WHERE singleton=1", rawNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
	}
	if minuteNS > oldMinute {
		if err = compactHour(ctx, tx, oldMinute, minuteNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE traffic_aggregate_retention SET minute_before_ns=? WHERE singleton=1", minuteNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
	}
	res, e := tx.ExecContext(ctx, "DELETE FROM traffic_aggregates_minute WHERE bucket_start < ? OR bucket_start >= ?", minuteNS, rawNS)
	if e != nil {
		return TrafficRetentionResult{}, safeError(ctx, e)
	}
	result.Minutes, err = res.RowsAffected()
	if err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_cost_aggregates_minute WHERE bucket_start < ? OR bucket_start >= ?", minuteNS, rawNS); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_dirty_hours WHERE bucket_start < ? OR bucket_start >= ?", minuteNS, rawNS); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if hourNS > oldHour {
		if err = compactDay(ctx, tx, oldHour, hourNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE traffic_aggregate_retention SET hour_before_ns=? WHERE singleton=1", hourNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
	}
	res, e = tx.ExecContext(ctx, "DELETE FROM traffic_aggregates_hour WHERE bucket_start < ? OR bucket_start >= ?", hourNS, minuteNS)
	if e != nil {
		return TrafficRetentionResult{}, safeError(ctx, e)
	}
	result.Hours, err = res.RowsAffected()
	if err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_cost_aggregates_hour WHERE bucket_start < ? OR bucket_start >= ?", hourNS, minuteNS); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_dirty_days WHERE bucket_start < ? OR bucket_start >= ?", hourNS, minuteNS); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	res, e = tx.ExecContext(ctx, "DELETE FROM traffic_aggregates_day WHERE bucket_start < ? OR bucket_start >= ?", dayNS, hourNS)
	if e != nil {
		return TrafficRetentionResult{}, safeError(ctx, e)
	}
	result.Days, err = res.RowsAffected()
	if err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_cost_aggregates_day WHERE bucket_start < ? OR bucket_start >= ?", dayNS, hourNS); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	if dayNS > oldDay {
		if _, err = tx.ExecContext(ctx, "UPDATE traffic_aggregate_retention SET day_before_ns=? WHERE singleton=1", dayNS); err != nil {
			return TrafficRetentionResult{}, safeError(ctx, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return TrafficRetentionResult{}, safeError(ctx, err)
	}
	return result, nil
}

func (s *Store) TrafficSummary(ctx context.Context, query TrafficQuery) (traffic.Summary, error) {
	if !query.valid(true) {
		return traffic.Summary{}, store.ErrInvalid
	}
	statement, args := analyticsQuery(query, "")
	var values [9]int64
	err := s.db.QueryRowContext(ctx, statement, args...).Scan(
		&values[0], &values[1], &values[2], &values[3], &values[4],
		&values[5], &values[6], &values[7], &values[8],
	)
	if err != nil {
		return traffic.Summary{}, safeError(ctx, err)
	}
	totals, err := trafficTotals(values)
	if err != nil {
		return traffic.Summary{}, err
	}
	costs, err := trafficCosts(ctx, s.db, query, false)
	if err != nil {
		return traffic.Summary{}, err
	}
	return traffic.Summary{From: query.From.UTC(), Until: query.Until.UTC(), Totals: totals, Costs: costs[0]}, nil
}

func (s *Store) TrafficTimeseries(ctx context.Context, query TrafficQuery) (traffic.Series, error) {
	if !query.valid(false) {
		return traffic.Series{}, store.ErrInvalid
	}
	statement, args := analyticsQuery(query, "series")
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return traffic.Series{}, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	points := make([]traffic.SeriesPoint, 0, int(query.Until.Sub(query.From)/query.Granularity))
	for rows.Next() {
		var bucket int64
		var values [9]int64
		if err = rows.Scan(&bucket, &values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8]); err != nil {
			return traffic.Series{}, safeError(ctx, err)
		}
		totals, totalsErr := trafficTotals(values)
		if totalsErr != nil {
			return traffic.Series{}, totalsErr
		}
		points = append(points, traffic.SeriesPoint{BucketStart: time.Unix(0, bucket).UTC(), Totals: totals})
	}
	if err = rows.Err(); err != nil {
		return traffic.Series{}, safeError(ctx, err)
	}
	if err = rows.Close(); err != nil {
		return traffic.Series{}, safeError(ctx, err)
	}
	costs, err := trafficCosts(ctx, s.db, query, true)
	if err != nil {
		return traffic.Series{}, err
	}
	for i := range points {
		points[i].Costs = costs[points[i].BucketStart.UnixNano()]
	}
	return traffic.Series{From: query.From.UTC(), Until: query.Until.UTC(), Granularity: granularityName(query.Granularity), Points: points}, nil
}

func trafficCosts(ctx context.Context, db *sql.DB, query TrafficQuery, series bool) (map[int64][]traffic.CostTotal, error) {
	statement, args := costAnalyticsQuery(query, series)
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	result := map[int64][]traffic.CostTotal{}
	for rows.Next() {
		var bucket int64
		var currency string
		var micros, upload, download int64
		if series {
			err = rows.Scan(&bucket, &currency, &micros, &upload, &download)
		} else {
			err = rows.Scan(&currency, &micros, &upload, &download)
		}
		amount := traffic.Money{Currency: currency, Micros: micros}
		if err != nil {
			return nil, safeError(ctx, err)
		}
		if amount.Validate() != nil || upload < 0 || download < 0 {
			return nil, store.ErrSchema
		}
		result[bucket] = append(result[bucket], traffic.CostTotal{
			Amount: amount, PricedUpstreamUpload: traffic.Bytes(upload), PricedUpstreamDownload: traffic.Bytes(download),
		})
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	if result[0] == nil && !series {
		result[0] = []traffic.CostTotal{}
	}
	return result, nil
}

func costAnalyticsQuery(query TrafficQuery, series bool) (string, []any) {
	const canonical = `WITH canonical_cost AS (
SELECT at AS sample_at,action,protocol,coalesce(client_id,'') AS client_id,coalesce(pool_id,'') AS pool_id,coalesce(proxy_id,'') AS proxy_id,cost_currency AS currency,cost_micros AS configured_cost_micros,upstream_upload AS priced_upstream_upload,upstream_download AS priced_upstream_download
FROM traffic_events WHERE at>=? AND at<? AND at>=(SELECT before_ns FROM traffic_retention WHERE singleton=1) AND cost_currency<>''
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download
FROM traffic_cost_aggregates_minute WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT minute_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT before_ns FROM traffic_retention WHERE singleton=1)
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download
FROM traffic_cost_aggregates_hour WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT hour_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT minute_before_ns FROM traffic_aggregate_retention WHERE singleton=1)
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download
FROM traffic_cost_aggregates_day WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT day_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT hour_before_ns FROM traffic_aggregate_retention WHERE singleton=1)
) `
	args := []any{
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
	}
	conditions := make([]string, 0, 5)
	for _, filter := range []struct {
		column string
		value  string
	}{{"client_id", string(query.ClientID)}, {"pool_id", string(query.PoolID)}, {"proxy_id", string(query.ProxyID)}, {"action", query.Action}, {"protocol", query.Protocol}} {
		if filter.value != "" {
			conditions = append(conditions, filter.column+"=?")
			args = append(args, filter.value)
		}
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	columns := `currency,sum(configured_cost_micros),sum(priced_upstream_upload),sum(priced_upstream_download)`
	if !series {
		return canonical + "SELECT " + columns + " FROM canonical_cost" + where + " GROUP BY currency ORDER BY currency", args
	}
	bucket := int64(query.Granularity)
	args = append(args[:8], append([]any{bucket, bucket}, args[8:]...)...)
	return canonical + "SELECT (sample_at / ?) * ? AS bucket_start," + columns + " FROM canonical_cost" + where + " GROUP BY 1,currency ORDER BY 1,currency", args
}

func analyticsQuery(query TrafficQuery, mode string) (string, []any) {
	const canonical = `WITH canonical AS (
SELECT at AS sample_at,action,protocol,coalesce(client_id,'') AS client_id,coalesce(pool_id,'') AS pool_id,coalesce(proxy_id,'') AS proxy_id,1 AS request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided
FROM traffic_events WHERE at>=? AND at<? AND at>=(SELECT before_ns FROM traffic_retention WHERE singleton=1)
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided
FROM traffic_aggregates_minute WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT minute_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT before_ns FROM traffic_retention WHERE singleton=1)
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided
FROM traffic_aggregates_hour WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT hour_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT minute_before_ns FROM traffic_aggregate_retention WHERE singleton=1)
UNION ALL
SELECT bucket_start AS sample_at,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided
FROM traffic_aggregates_day WHERE bucket_start>=? AND bucket_start<? AND bucket_start>=(SELECT day_before_ns FROM traffic_aggregate_retention WHERE singleton=1) AND bucket_start<(SELECT hour_before_ns FROM traffic_aggregate_retention WHERE singleton=1)
) `
	args := []any{
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
		query.From.UTC().UnixNano(), query.Until.UTC().UnixNano(),
	}
	conditions := make([]string, 0, 5)
	for _, filter := range []struct {
		column string
		value  string
	}{{"client_id", string(query.ClientID)}, {"pool_id", string(query.PoolID)}, {"proxy_id", string(query.ProxyID)}, {"action", query.Action}, {"protocol", query.Protocol}} {
		if filter.value != "" {
			conditions = append(conditions, filter.column+"=?")
			args = append(args, filter.value)
		}
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	columns := `coalesce(sum(request_count),0),coalesce(sum(client_upload),0),coalesce(sum(client_download),0),coalesce(sum(upstream_upload),0),coalesce(sum(upstream_download),0),coalesce(sum(direct_bytes),0),coalesce(sum(cache_served),0),coalesce(sum(health_check),0),coalesce(sum(estimated_avoided),0)`
	if mode != "series" {
		return canonical + "SELECT " + columns + " FROM canonical" + where, args
	}
	bucket := int64(query.Granularity)
	// Bucket parameters occur after the canonical CTE parameters but before the
	// optional filters in SQL, so insert them at the matching argument position.
	args = append(args[:8], append([]any{bucket, bucket}, args[8:]...)...)
	return canonical + "SELECT (sample_at / ?) * ? AS bucket_start," + columns + " FROM canonical" + where + " GROUP BY 1 ORDER BY 1", args
}

func trafficTotals(values [9]int64) (traffic.Totals, error) {
	for _, value := range values {
		if value < 0 {
			return traffic.Totals{}, store.ErrSchema
		}
	}
	return traffic.Totals{
		RequestCount: values[0], ClientUpload: traffic.Bytes(values[1]), ClientDownload: traffic.Bytes(values[2]),
		UpstreamUpload: traffic.Bytes(values[3]), UpstreamDownload: traffic.Bytes(values[4]), Direct: traffic.Bytes(values[5]),
		CacheServed: traffic.Bytes(values[6]), HealthCheck: traffic.Bytes(values[7]), EstimatedAvoided: traffic.Bytes(values[8]),
	}, nil
}

func granularityName(value time.Duration) string {
	switch value {
	case time.Minute:
		return "minute"
	case time.Hour:
		return "hour"
	default:
		return "day"
	}
}

func validTrafficTime(at time.Time) bool {
	return !at.Before(time.Unix(0, 0)) && time.Unix(0, at.UnixNano()).Equal(at)
}
func scanTraffic(scanner interface{ Scan(...any) error }) (traffic.Event, error) {
	var event traffic.Event
	var at int64
	var client, pool, proxy sql.NullString
	var upload, download, upstreamUpload, upstreamDownload, direct, cacheServed, health, avoided int64
	var currency string
	var costMicros, priceMicros, unitBytes, effectiveAt int64
	var downloadOnly bool
	err := scanner.Scan(&at, &event.RequestID, &event.ConnectionID, &client, &pool, &proxy, &event.Host, &event.Protocol, &event.Action, &event.StatusCode, &upload, &download, &upstreamUpload, &upstreamDownload, &direct, &cacheServed, &health, &avoided, &currency, &costMicros, &priceMicros, &unitBytes, &downloadOnly, &effectiveAt)
	if err != nil {
		return traffic.Event{}, err
	}
	event.At = time.Unix(0, at).UTC()
	if client.Valid {
		event.ClientID = model.ID(client.String)
	}
	if pool.Valid {
		event.PoolID = model.ID(pool.String)
	}
	if proxy.Valid {
		event.ProxyID = model.ID(proxy.String)
	}
	event.ClientUpload = traffic.Bytes(upload)
	event.ClientDownload = traffic.Bytes(download)
	event.UpstreamUpload = traffic.Bytes(upstreamUpload)
	event.UpstreamDownload = traffic.Bytes(upstreamDownload)
	event.Direct = traffic.Bytes(direct)
	event.CacheServed = traffic.Bytes(cacheServed)
	event.HealthCheck = traffic.Bytes(health)
	event.EstimatedAvoided = traffic.Bytes(avoided)
	if currency != "" {
		event.ConfiguredCost = &traffic.CostSnapshot{
			Amount: traffic.Money{Currency: currency, Micros: costMicros},
			Rate: traffic.Rate{
				Price: traffic.Money{Currency: currency, Micros: priceMicros}, Unit: traffic.ByteUnit(unitBytes),
				DownloadOnly: downloadOnly, EffectiveAt: time.Unix(0, effectiveAt).UTC(),
			},
		}
	}
	if event.Validate() != nil {
		return traffic.Event{}, store.ErrSchema
	}
	return event, nil
}

func trafficCostValues(cost *traffic.CostSnapshot) (string, int64, int64, int64, bool, int64) {
	if cost == nil {
		return "", 0, 0, 0, false, 0
	}
	return cost.Amount.Currency, cost.Amount.Micros, cost.Rate.Price.Micros, int64(cost.Rate.Unit), cost.Rate.DownloadOnly, cost.Rate.EffectiveAt.UTC().UnixNano()
}
