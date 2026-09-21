package sqlite

import (
	"database/sql"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"github.com/nguyenduytan/proxysieve/pkg/traffic"
	"io/fs"
	"math"
	"testing"
	"testing/fstest"
	"time"
)

func TestTrafficPersistenceRollupAndRetention(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().UTC().Truncate(time.Minute)
	for _, id := range []string{"one", "two"} {
		event := traffic.Event{At: now, RequestID: trafficID(id), ConnectionID: trafficID("conn" + id), Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 10}
		if err = s.RecordTraffic(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListTraffic(t.Context(), TrafficPage{Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	if err = s.RollupMinute(t.Context(), now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count, total int
	if err = s.db.QueryRow("SELECT request_count,upstream_download FROM traffic_aggregates_minute").Scan(&count, &total); err != nil || count != 2 || total != 20 {
		t.Fatal(count, total, err)
	}
	if err = s.RollupMinute(t.Context(), now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT request_count,upstream_download FROM traffic_aggregates_minute").Scan(&count, &total); err != nil || count != 2 || total != 20 {
		t.Fatal(count, total, err)
	}
	deleted, err := s.RetainTraffic(t.Context(), now.Add(time.Minute))
	if err != nil || deleted != 2 {
		t.Fatal(deleted, err)
	}
}

func TestTrafficChainDimensionSurvivesRollupAndRawRetention(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	for _, event := range []traffic.Event{
		{At: base.Add(time.Second), RequestID: "chain-request", ConnectionID: "chain-connection", PoolID: "first-pool", ProxyID: "last-proxy", ChainID: "privacy-chain", Host: "example.invalid", Protocol: "http", Action: "chain", StatusCode: 200, UpstreamDownload: 10},
		{At: base.Add(2 * time.Second), RequestID: "proxy-request", ConnectionID: "proxy-connection", PoolID: "first-pool", ProxyID: "last-proxy", Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 20},
	} {
		if err = s.RecordTraffic(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListTraffic(t.Context(), TrafficPage{Limit: 10})
	if err != nil || len(events) != 2 || events[1].ChainID != "privacy-chain" {
		t.Fatal(events, err)
	}
	if _, err = s.RetainTraffic(t.Context(), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	query := TrafficQuery{From: base, Until: base.Add(time.Hour), ChainID: "privacy-chain"}
	summary, err := s.TrafficSummary(t.Context(), query)
	if err != nil || summary.Totals.RequestCount != 1 || summary.Totals.UpstreamDownload != 10 {
		t.Fatal(summary, err)
	}
	other, err := s.TrafficSummary(t.Context(), TrafficQuery{From: base, Until: base.Add(time.Hour), Action: "proxy"})
	if err != nil || other.Totals.RequestCount != 1 || other.Totals.UpstreamDownload != 20 {
		t.Fatal(other, err)
	}
}
func trafficID(value string) model.ID { return model.ID(value) }

func TestTrafficBatchIsAtomic(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().UTC()
	first := traffic.Event{At: now, RequestID: "first", ConnectionID: "connection", Host: "example.invalid", Protocol: "http", Action: "proxy"}
	second := first
	second.RequestID = "second"
	if err = s.RecordTrafficBatch(t.Context(), []traffic.Event{first, second}); err != nil {
		t.Fatal(err)
	}
	invalid := second
	invalid.RequestID = ""
	if err = s.RecordTrafficBatch(t.Context(), []traffic.Event{first, invalid}); !errors.Is(err, traffic.ErrInvalidEvent) {
		t.Fatal(err)
	}
	var events, dirty int
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_events").Scan(&events); err != nil || events != 2 {
		t.Fatal(events, err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_dirty_minutes").Scan(&dirty); err != nil || dirty != 1 {
		t.Fatal(dirty, err)
	}
	if err = s.RecordTrafficBatch(t.Context(), nil); !errors.Is(err, traffic.ErrInvalidEvent) {
		t.Fatal(err)
	}
}

func TestTrafficRetentionCatchesUpAndSurvivesRestart(t *testing.T) {
	path := tempDatabase(t)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{base, base.Add(59 * time.Second), base.Add(time.Minute), base.Add(90 * time.Second)} {
		event := traffic.Event{At: at, RequestID: "request", ConnectionID: "connection", Protocol: "http", Action: "proxy", UpstreamDownload: traffic.Bytes(i + 1)}
		if err = s.RecordTraffic(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	// Retention must aggregate old traffic even when the scheduler never ran.
	if n, err := s.RetainTraffic(t.Context(), base.Add(90*time.Second)); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	// Rebuilding from before the retired range must not erase its aggregates.
	if err = s.RollupMinute(t.Context(), base, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var count, total int
	if err = s.db.QueryRow("SELECT sum(request_count),sum(upstream_download) FROM traffic_aggregates_minute").Scan(&count, &total); err != nil || count != 4 || total != 10 {
		t.Fatal(count, total, err)
	}
	events, err := s.ListTraffic(t.Context(), TrafficPage{Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	late := events[0]
	late.At = base
	if err = s.RecordTraffic(t.Context(), late); !errors.Is(err, traffic.ErrInvalidEvent) {
		t.Fatal(err)
	}
	// A backward clock/config change cannot move the watermark backwards.
	if n, err := s.RetainTraffic(t.Context(), base); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err = s.RecordTraffic(t.Context(), late); !errors.Is(err, traffic.ErrInvalidEvent) {
		t.Fatal(err)
	}
	if n, err := s.RetainTraffic(t.Context(), base.Add(2*time.Minute)); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if n, err := s.RetainTraffic(t.Context(), base.Add(2*time.Minute)); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err = s.RollupMinute(t.Context(), base, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT sum(request_count),sum(upstream_download) FROM traffic_aggregates_minute").Scan(&count, &total); err != nil || count != 4 || total != 10 {
		t.Fatal(count, total, err)
	}
}

func TestTrafficRollupRejectsPartialBucketsAndRollsBackOverflow(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	event := traffic.Event{At: base, RequestID: "request", ConnectionID: "connection", Protocol: "http", Action: "proxy", UpstreamDownload: traffic.Bytes(math.MaxInt64)}
	if err = s.RecordTraffic(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupMinute(t.Context(), base, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, window := range [][2]time.Time{{base.Add(time.Second), base.Add(time.Minute)}, {base, base.Add(time.Second)}, {base, base}, {time.Unix(-60, 0), base}, {base, time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)}} {
		if err = s.RollupMinute(t.Context(), window[0], window[1]); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(window, err)
		}
	}
	event.UpstreamDownload = 1
	if err = s.RecordTraffic(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetainTraffic(t.Context(), base.Add(time.Minute)); !errors.Is(err, store.ErrUnavailable) {
		t.Fatal(err)
	}
	// Both original aggregate and raw records survive a failed SUM.
	var total int64
	if err = s.db.QueryRow("SELECT upstream_download FROM traffic_aggregates_minute").Scan(&total); err != nil || total != math.MaxInt64 {
		t.Fatal(total, err)
	}
	events, err := s.ListTraffic(t.Context(), TrafficPage{Limit: 10})
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	var retired int64
	if err = s.db.QueryRow("SELECT before_ns FROM traffic_retention").Scan(&retired); err != nil || retired != 0 {
		t.Fatal(retired, err)
	}
}

func TestTrafficUpgradeSeedsDirtyBucketsAndLateArrivals(t *testing.T) {
	// Build an actual schema-5 database, then upgrade through Open.
	path := tempDatabase(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{db: db}
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names[:5] {
		data, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if err = old.migrate(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err = db.Exec(`INSERT INTO traffic_events(at,request_id,connection_id,host,protocol,action,status_code,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided) VALUES (?,'request','connection','example.invalid','http','proxy',200,0,0,0,10,0,0,0,0)`, base.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err = s.RollupMinute(t.Context(), base, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var dirty int
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_dirty_minutes").Scan(&dirty); err != nil || dirty != 0 {
		t.Fatal(dirty, err)
	}
	// Late traffic must re-open its complete bucket without adding the old
	// aggregate to itself or rebuilding unrelated, unchanged buckets.
	event := traffic.Event{At: base.Add(30 * time.Second), RequestID: "late", ConnectionID: "connection", Host: "example.invalid", Protocol: "http", Action: "proxy", StatusCode: 200, UpstreamDownload: 5}
	if err = s.RecordTraffic(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_dirty_minutes").Scan(&dirty); err != nil || dirty != 1 {
		t.Fatal(dirty, err)
	}
	for range 2 {
		if err = s.RollupMinute(t.Context(), base, base.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		var count, total int
		if err = s.db.QueryRow("SELECT request_count,upstream_download FROM traffic_aggregates_minute").Scan(&count, &total); err != nil || count != 2 || total != 15 {
			t.Fatal(count, total, err)
		}
	}
}

func TestSchemaSeventeenUpgradePreservesTrafficAndCostAggregates(t *testing.T) {
	path := tempDatabase(t)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := &Store{db: database}
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil || len(names) != 22 {
		t.Fatal(names, err)
	}
	for _, name := range names[:16] {
		data, readErr := migrations.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[name] = &fstest.MapFile{Data: data}
	}
	if err = old.migrate(t.Context(), files); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"traffic_aggregates_minute", "traffic_aggregates_hour", "traffic_aggregates_day"} {
		_, err = database.Exec(`INSERT INTO ` + table + ` (bucket_start,action,protocol,client_id,pool_id,proxy_id,request_count,client_upload,client_download,upstream_upload,upstream_download,direct_bytes,cache_served,health_check,estimated_avoided) VALUES (1,'proxy','http','client','pool','proxy',2,3,4,5,6,7,8,9,10)`)
		if err != nil {
			t.Fatal(table, err)
		}
	}
	for _, table := range []string{"traffic_cost_aggregates_minute", "traffic_cost_aggregates_hour", "traffic_cost_aggregates_day"} {
		_, err = database.Exec(`INSERT INTO ` + table + ` (bucket_start,action,protocol,client_id,pool_id,proxy_id,currency,configured_cost_micros,priced_upstream_upload,priced_upstream_download) VALUES (1,'proxy','http','client','pool','proxy','USD',11,12,13)`)
		if err != nil {
			t.Fatal(table, err)
		}
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	for _, table := range []string{"traffic_aggregates_minute", "traffic_aggregates_hour", "traffic_aggregates_day"} {
		var chain, policyID, ruleID string
		var requests, download int64
		if err = repository.db.QueryRow(`SELECT chain_id,policy_id,rule_id,request_count,upstream_download FROM `+table).Scan(&chain, &policyID, &ruleID, &requests, &download); err != nil || chain != "" || policyID != "" || ruleID != "" || requests != 2 || download != 6 {
			t.Fatal(table, chain, policyID, ruleID, requests, download, err)
		}
	}
	for _, table := range []string{"traffic_cost_aggregates_minute", "traffic_cost_aggregates_hour", "traffic_cost_aggregates_day"} {
		var chain, policyID, ruleID, currency string
		var cost int64
		if err = repository.db.QueryRow(`SELECT chain_id,policy_id,rule_id,currency,configured_cost_micros FROM `+table).Scan(&chain, &policyID, &ruleID, &currency, &cost); err != nil || chain != "" || policyID != "" || ruleID != "" || currency != "USD" || cost != 11 {
			t.Fatal(table, chain, policyID, ruleID, currency, cost, err)
		}
	}
}

func TestTrafficHierarchySummaryAndTimeseries(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events := []traffic.Event{
		{At: base.Add(10 * time.Minute), RequestID: "first", ConnectionID: "connection", ClientID: "client-a", PoolID: "pool-a", ProxyID: "proxy-a", Protocol: "http", Action: "proxy", UpstreamUpload: 2, UpstreamDownload: 8},
		{At: base.Add(70 * time.Minute), RequestID: "second", ConnectionID: "connection", ClientID: "client-b", Protocol: "http", Action: "reject", Direct: 5},
	}
	if err = s.RecordTrafficBatch(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	query := TrafficQuery{From: base, Until: base.Add(2 * time.Hour)}
	summary, err := s.TrafficSummary(t.Context(), query)
	if err != nil || summary.Totals.RequestCount != 2 || summary.Totals.UpstreamDownload != 8 || summary.Totals.Direct != 5 {
		t.Fatal(summary, err)
	}
	filtered, err := s.TrafficSummary(t.Context(), TrafficQuery{From: base, Until: base.Add(2 * time.Hour), ClientID: "client-a", Action: "proxy"})
	if err != nil || filtered.Totals.RequestCount != 1 || filtered.Totals.UpstreamUpload != 2 {
		t.Fatal(filtered, err)
	}
	if err = s.RollupMinute(t.Context(), base, base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupHour(t.Context(), base, base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupDay(t.Context(), base, base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var hourCount, dayCount, hourBytes, dayBytes int64
	if err = s.db.QueryRow("SELECT sum(request_count),sum(upstream_download) FROM traffic_aggregates_hour").Scan(&hourCount, &hourBytes); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT sum(request_count),sum(upstream_download) FROM traffic_aggregates_day").Scan(&dayCount, &dayBytes); err != nil {
		t.Fatal(err)
	}
	if hourCount != 2 || dayCount != 2 || hourBytes != 8 || dayBytes != 8 {
		t.Fatal(hourCount, dayCount, hourBytes, dayBytes)
	}
	late := events[0]
	late.At = base.Add(20 * time.Minute)
	late.RequestID = "late"
	late.UpstreamUpload = 0
	late.UpstreamDownload = 1
	if err = s.RecordTraffic(t.Context(), late); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupMinute(t.Context(), base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupHour(t.Context(), base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupDay(t.Context(), base, base.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT sum(request_count),sum(upstream_download) FROM traffic_aggregates_day").Scan(&dayCount, &dayBytes); err != nil || dayCount != 3 || dayBytes != 9 {
		t.Fatal(dayCount, dayBytes, err)
	}
	series, err := s.TrafficTimeseries(t.Context(), TrafficQuery{From: base, Until: base.Add(2 * time.Hour), Granularity: time.Hour})
	if err != nil || len(series.Points) != 2 || series.Points[0].Totals.RequestCount != 2 || series.Points[0].Totals.UpstreamDownload != 9 || series.Points[1].Totals.Direct != 5 {
		t.Fatal(series, err)
	}
	summary, err = s.TrafficSummary(t.Context(), query)
	if err != nil || summary.Totals.RequestCount != 3 || summary.Totals.UpstreamDownload != 9 {
		t.Fatal(summary, err)
	}
	if deleted, retainErr := s.RetainTraffic(t.Context(), base.Add(2*time.Hour)); retainErr != nil || deleted != 3 {
		t.Fatal(deleted, retainErr)
	}
	retained, err := s.TrafficSummary(t.Context(), query)
	if err != nil || retained.Totals != summary.Totals {
		t.Fatal(retained, summary, err)
	}
	for _, invalid := range []TrafficQuery{
		{From: base.Add(time.Second), Until: base.Add(time.Hour)},
		{From: base, Until: base.Add(time.Hour), Granularity: 30 * time.Minute},
		{From: base, Until: base.Add(2001 * time.Hour), Granularity: time.Hour},
		{From: base, Until: base.Add(time.Hour), ClientID: "not valid"},
	} {
		if invalid.Granularity == 0 {
			_, err = s.TrafficSummary(t.Context(), invalid)
		} else {
			_, err = s.TrafficTimeseries(t.Context(), invalid)
		}
		if !errors.Is(err, store.ErrInvalid) {
			t.Fatal(invalid, err)
		}
	}
}

func TestTrafficTierRetentionKeepsCanonicalTotals(t *testing.T) {
	path := tempDatabase(t)
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events := []traffic.Event{{At: base.Add(-time.Hour), RequestID: "before", ConnectionID: "connection", Protocol: "http", Action: "proxy", UpstreamDownload: 1}}
	for day := 1; day <= 5; day++ {
		events = append(events, traffic.Event{At: base.Add(time.Duration(day)*24*time.Hour + time.Hour), RequestID: trafficID("day" + string(rune('0'+day))), ConnectionID: "connection", Protocol: "http", Action: "proxy", UpstreamDownload: traffic.Bytes(day)})
	}
	if err = s.RecordTrafficBatch(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupMinute(t.Context(), base.Add(-24*time.Hour), base.Add(6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupHour(t.Context(), base.Add(-24*time.Hour), base.Add(6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupDay(t.Context(), base.Add(-24*time.Hour), base.Add(6*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	before, err := s.TrafficSummary(t.Context(), TrafficQuery{From: base, Until: base.Add(6 * 24 * time.Hour)})
	if err != nil || before.Totals.RequestCount != 5 || before.Totals.UpstreamDownload != 15 {
		t.Fatal(before, err)
	}
	result, err := s.RetainTrafficTiers(t.Context(), TrafficRetention{
		RawBefore: base.Add(5 * 24 * time.Hour), MinuteBefore: base.Add(4 * 24 * time.Hour),
		HourBefore: base.Add(3 * 24 * time.Hour), DayBefore: base,
	})
	if err != nil || result.RawEvents != 5 || result.Minutes == 0 || result.Hours == 0 || result.Days == 0 {
		t.Fatal(result, err)
	}
	var raw, minutes, hours, days int
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_events").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_aggregates_minute").Scan(&minutes); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_aggregates_hour").Scan(&hours); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT count(*) FROM traffic_aggregates_day").Scan(&days); err != nil {
		t.Fatal(err)
	}
	if raw != 1 || minutes != 1 || hours != 1 || days != 2 {
		t.Fatalf("tier rows raw=%d minute=%d hour=%d day=%d", raw, minutes, hours, days)
	}
	after, err := s.TrafficSummary(t.Context(), TrafficQuery{From: base, Until: base.Add(6 * 24 * time.Hour)})
	if err != nil || after.Totals != before.Totals {
		t.Fatal(after, before, err)
	}
	series, err := s.TrafficTimeseries(t.Context(), TrafficQuery{From: base, Until: base.Add(6 * 24 * time.Hour), Granularity: 24 * time.Hour})
	if err != nil || len(series.Points) != 5 {
		t.Fatal(series, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.TrafficSummary(t.Context(), TrafficQuery{From: base, Until: base.Add(6 * 24 * time.Hour)}); err != nil || again.Totals != before.Totals {
		t.Fatal(again, err)
	}
	backward, err := s.RetainTrafficTiers(t.Context(), TrafficRetention{RawBefore: base, MinuteBefore: base, HourBefore: base, DayBefore: base})
	if err != nil || backward != (TrafficRetentionResult{}) {
		t.Fatal(backward, err)
	}
	var rawBefore, minuteBefore, hourBefore, dayBefore int64
	if err = s.db.QueryRow(`SELECT raw.before_ns,tiers.minute_before_ns,tiers.hour_before_ns,tiers.day_before_ns
FROM traffic_retention AS raw CROSS JOIN traffic_aggregate_retention AS tiers`).Scan(&rawBefore, &minuteBefore, &hourBefore, &dayBefore); err != nil {
		t.Fatal(err)
	}
	if rawBefore != base.Add(5*24*time.Hour).UnixNano() || minuteBefore != base.Add(4*24*time.Hour).UnixNano() || hourBefore != base.Add(3*24*time.Hour).UnixNano() || dayBefore != base.UnixNano() {
		t.Fatal(rawBefore, minuteBefore, hourBefore, dayBefore)
	}
	if _, err = s.RetainTrafficTiers(t.Context(), TrafficRetention{}); !errors.Is(err, store.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestConfiguredCostsKeepCurrenciesAndSurviveTierRetention(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	usdRate := traffic.Rate{Price: traffic.Money{Currency: "USD", Micros: 2_000_000}, Unit: traffic.GB, EffectiveAt: base}
	eurRate := traffic.Rate{Price: traffic.Money{Currency: "EUR", Micros: 4_000_000}, Unit: traffic.GB, DownloadOnly: true, EffectiveAt: base.Add(24 * time.Hour)}
	usdCost, err := traffic.NewCostSnapshot(&usdRate, 250_000_000, 250_000_000)
	if err != nil {
		t.Fatal(err)
	}
	eurCost, err := traffic.NewCostSnapshot(&eurRate, 900_000_000, 250_000_000)
	if err != nil {
		t.Fatal(err)
	}
	events := []traffic.Event{
		{At: base.Add(25 * time.Hour), RequestID: "usd", ConnectionID: "connection", ClientID: "client", PoolID: "pool", ProxyID: "proxy-usd", Protocol: "http", Action: "proxy", UpstreamUpload: 250_000_000, UpstreamDownload: 250_000_000, ConfiguredCost: usdCost},
		{At: base.Add(49 * time.Hour), RequestID: "eur", ConnectionID: "connection", ClientID: "client", PoolID: "pool", ProxyID: "proxy-eur", Protocol: "connect", Action: "proxy", UpstreamUpload: 900_000_000, UpstreamDownload: 250_000_000, ConfiguredCost: eurCost},
	}
	if err = s.RecordTrafficBatch(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	query := TrafficQuery{From: base.Add(24 * time.Hour), Until: base.Add(72 * time.Hour)}
	assertCosts := func(summary traffic.Summary) {
		t.Helper()
		if len(summary.Costs) != 2 || summary.Costs[0].Amount != (traffic.Money{Currency: "EUR", Micros: 1_000_000}) || summary.Costs[1].Amount != (traffic.Money{Currency: "USD", Micros: 1_000_000}) {
			t.Fatalf("unexpected configured costs: %+v", summary.Costs)
		}
		if summary.Costs[0].PricedUpstreamUpload != 900_000_000 || summary.Costs[0].PricedUpstreamDownload != 250_000_000 || summary.Costs[1].PricedUpstreamUpload != 250_000_000 || summary.Costs[1].PricedUpstreamDownload != 250_000_000 {
			t.Fatalf("unexpected priced byte coverage: %+v", summary.Costs)
		}
	}
	before, err := s.TrafficSummary(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	assertCosts(before)
	if err = s.RollupMinute(t.Context(), base.Add(24*time.Hour), base.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupHour(t.Context(), base.Add(24*time.Hour), base.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.RollupDay(t.Context(), base.Add(24*time.Hour), base.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetainTrafficTiers(t.Context(), TrafficRetention{RawBefore: base.Add(72 * time.Hour), MinuteBefore: base.Add(72 * time.Hour), HourBefore: base.Add(72 * time.Hour), DayBefore: base}); err != nil {
		t.Fatal(err)
	}
	after, err := s.TrafficSummary(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	assertCosts(after)
	series, err := s.TrafficTimeseries(t.Context(), TrafficQuery{From: query.From, Until: query.Until, Granularity: 24 * time.Hour})
	if err != nil || len(series.Points) != 2 || len(series.Points[0].Costs) != 1 || len(series.Points[1].Costs) != 1 {
		t.Fatal(series, err)
	}
	filtered, err := s.TrafficSummary(t.Context(), TrafficQuery{From: query.From, Until: query.Until, ProxyID: "proxy-usd"})
	if err != nil || len(filtered.Costs) != 1 || filtered.Costs[0].Amount.Currency != "USD" {
		t.Fatal(filtered, err)
	}
}

func TestTrafficPolicyRuleDimensionsAndBreakdownSurviveRetention(t *testing.T) {
	s, err := Open(t.Context(), tempDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rate := traffic.Rate{Price: traffic.Money{Currency: "USD", Micros: 1_000_000_000}, Unit: traffic.GB, EffectiveAt: base}
	cost, err := traffic.NewCostSnapshot(&rate, 0, 300)
	if err != nil {
		t.Fatal(err)
	}
	events := []traffic.Event{
		{At: base.Add(time.Second), RequestID: "paid-a", ConnectionID: "connection-a", PolicyID: "policy-a", RuleID: "proxy-rule", PoolID: "paid-pool", ProxyID: "proxy-a", Protocol: "http", Action: "proxy", UpstreamDownload: 300, ConfiguredCost: cost},
		{At: base.Add(2 * time.Second), RequestID: "paid-b", ConnectionID: "connection-b", PolicyID: "policy-b", RuleID: "other-rule", PoolID: "other-pool", ProxyID: "proxy-b", Protocol: "http", Action: "proxy", UpstreamDownload: 100},
		{At: base.Add(3 * time.Second), RequestID: "blocked-a", ConnectionID: "connection-c", PolicyID: "policy-a", RuleID: "block-rule", Protocol: "http", Action: "block"},
		{At: base.Add(4 * time.Second), RequestID: "blocked-b", ConnectionID: "connection-d", PolicyID: "policy-a", RuleID: "block-rule", Protocol: "socks5", Action: "block"},
	}
	if err = s.RecordTrafficBatch(t.Context(), events); err != nil {
		t.Fatal(err)
	}
	stored, err := s.ListTraffic(t.Context(), TrafficPage{Limit: 10})
	if err != nil || len(stored) != 4 || stored[0].PolicyID != "policy-a" || stored[0].RuleID != "block-rule" {
		t.Fatal(stored, err)
	}
	query := TrafficQuery{From: base, Until: base.Add(time.Hour)}
	filtered, err := s.TrafficSummary(t.Context(), TrafficQuery{From: query.From, Until: query.Until, PolicyID: "policy-a", RuleID: "block-rule", Action: "block"})
	if err != nil || filtered.Totals.RequestCount != 2 {
		t.Fatal(filtered, err)
	}
	assertBreakdowns := func() {
		t.Helper()
		pools, breakdownErr := s.TrafficBreakdown(t.Context(), query, "pool", 1)
		if breakdownErr != nil || len(pools.Items) != 1 || pools.Items[0].Value != "paid-pool" || pools.Items[0].Totals.UpstreamDownload != 300 || len(pools.Items[0].Costs) != 1 || pools.Items[0].Costs[0].Amount.Micros != 300 {
			t.Fatal(pools, breakdownErr)
		}
		blocked, breakdownErr := s.TrafficBreakdown(t.Context(), TrafficQuery{From: query.From, Until: query.Until, Action: "block"}, "rule", 10)
		if breakdownErr != nil || len(blocked.Items) != 1 || blocked.Items[0].Value != "block-rule" || blocked.Items[0].Totals.RequestCount != 2 {
			t.Fatal(blocked, breakdownErr)
		}
	}
	assertBreakdowns()
	if err = s.RollupMinute(t.Context(), base, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetainTraffic(t.Context(), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertBreakdowns()
	for _, invalid := range []struct {
		dimension string
		limit     int
	}{{"host", 10}, {"pool", 0}, {"pool", 101}} {
		if _, err = s.TrafficBreakdown(t.Context(), query, invalid.dimension, invalid.limit); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(invalid, err)
		}
	}
}
