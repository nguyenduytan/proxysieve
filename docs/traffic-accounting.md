# Traffic Accounting Foundation

Current gateway events count successful HTTP application-stream reads/writes at
ProxySieve's client/upstream boundaries. Partial I/O that returns an error still
adds its successful byte count. The event includes client upload/download, upstream
upload/download, direct bytes, health-check bytes and estimated avoided bytes as
separate values.

These numbers are **not** a NIC, TCP/IP, TLS or provider-billing meter. Provider
framing, rounding and measurement points may differ. Costs use fixed-point currency
and explicit decimal GB/GiB rate snapshots; no floating-point value is authoritative.

Live events use a bounded newest-event memory buffer and are authenticated through
the local API. When that buffer is full, the oldest item is replaced and the
eviction counter advances; live updates therefore do not freeze permanently at
capacity. When the admin control plane is enabled, gateway events also enter a bounded
asynchronous queue for batched SQLite persistence. Request handlers never wait for
that SQLite write. `/api/v1/traffic/history` returns recent persisted events.
Admin-disabled runtime currently remains memory-only.

The live API exposes durable queue depth, accepted/written totals, queue drops,
failed-event totals and write-failure count. Queue overflow is fail-fast and
observable. A failed batch is counted as lost analytics data and is not retried;
it never changes the gateway route decision. On orderly shutdown, ProxySieve stops
accepting events, drains accepted work, joins the writer, then closes SQLite.
Individual database calls have a five-second deadline. Process crashes can still
lose queued events. This analytics path is not hard-budget enforcement and must
not be treated as lossless provider-billed usage.

## Hierarchical aggregation and retention foundation

- Schema 6 adds a durable retention watermark and a transactional dirty-minute
  index. Upgrades seed that index from existing raw events. Only changed complete
  minute buckets are rebuilt, including late arrivals and downtime catch-up.
- Rollup windows must be UTC-equivalent whole-minute boundaries and use the
  half-open interval `[from, until)`. Partial-minute windows are rejected rather
  than silently replacing a whole bucket with incomplete totals.
- Retention aggregates eligible old records, removes the raw records, and advances
  the watermark in one transaction. Failure (including integer SUM overflow)
  rolls back all three. The cutoff is rounded down to a minute, retaining less
  than one additional minute of raw data.
- Retired aggregates are not rebuilt after restart. New events older than the
  watermark are rejected, and a backward clock or retention-setting change cannot
  move that watermark backwards. Increasing retention cannot recover deleted raw
  records. Data already lost by an older implementation cannot be recovered.
- Event timestamps must fit nonnegative signed 64-bit Unix nanoseconds. Counters
  must fit nonnegative signed 64-bit integers; overflow is an error, never a
  floating-point conversion or a wrapped counter.
- The scheduler catches up at startup, then uses `traffic.aggregation_interval`.
  Jobs have a 30-second context deadline. `Stop` cancels and joins the worker before
  the runtime closes SQLite. Failed runs increment `Runner.Failures()`; this is
  not yet surfaced in the dashboard or operational metrics.
- Schema 7 adds hour and UTC-day aggregate tables. Minute changes mark their hour
  dirty, hour changes mark their day dirty, and completed tiers are rebuilt in
  order. This preserves idempotence and lets late events propagate without
  scanning unrelated buckets.

## Bounded analytics API

`GET /api/v1/traffic/summary` and `GET /api/v1/traffic/timeseries` are
authenticated. Both accept `from` and `until` RFC 3339 timestamps plus optional
`client_id`, `pool_id`, `proxy_id`, `chain_id`, `action` and `protocol` filters. Timeseries
also accepts `granularity=minute|hour|day`.

Ranges are half-open, must align to the chosen bucket (one minute for summary),
and cannot exceed ten years. Timeseries is additionally limited to 2,000 buckets.
When no range is supplied, the API returns the current 24-hour window. A supplied
range must include both endpoints. The query combines four non-overlapping
authoritative ranges: raw events, minute aggregates, hour aggregates, and UTC-day
aggregates, selected by their durable tier watermarks. Rows are neither omitted nor
counted twice as retention advances. Empty buckets are omitted by the API;
the Admin Panel fills them only for chart layout and never invents usage.

Schema 9 stores the exact endpoint-rate snapshot and configured estimated cost on
each priced event. Separate minute/hour/day cost tables group by ISO currency, so
USD and EUR are never added together when rates change. The same dirty-bucket and
transactional retention path rebuilds traffic and cost aggregates together.

Summary and timeseries responses expose `configured_costs` as one entry per
currency. Each entry also reports the upstream upload/download bytes covered by a
rate. Routes without an active rate remain in ordinary traffic totals but are not
silently presented as zero-cost traffic. A future-dated rate only becomes active at
its `effective_at` timestamp.

Schema 17 adds `chain_id` to raw traffic plus minute/hour/day traffic and cost
aggregates. Existing rows upgrade with an empty chain ID and retain their totals.
Chain routes record one end-to-end byte stream, not one duplicate event per hop.
Per-hop configured costs remain unpriced because the current event model stores
one rate snapshot; presenting the last hop's rate as the whole chain would be
misleading.

Verified regression cases include schema-5 upgrade through schema 9, restart, late arrival,
idempotent rollup/retention, partial cutoff preservation, aggregate overflow
rollback, atomic batched writes, queue saturation/write failure, shutdown drain,
HTTP-to-SQLite integration, raw/aggregate query continuity, bounded filters and
concurrent scheduler/recorder lifecycle under the race detector. Policy-blocked
visible HTTP requests are recorded with zero bytes; no avoided-byte estimate is
invented.

HTTP CONNECT and SOCKS5 CONNECT now record bytes transferred after their downstream
handshake. Client read/write counts remain separate from bytes successfully read
from or written to the selected route. Paid proxy routes populate upstream stream
counters; direct routes populate `direct_bytes`. These totals do not include TCP/IP,
TLS, HTTP CONNECT or SOCKS framing and are not provider invoice measurements.

HTTP, CONNECT, and SOCKS5 retries emit one event per attempted route. Those events
share request/connection IDs for correlation but retain the selected pool, proxy,
chain, status, and configured-cost snapshot for that attempt. Failed pre-response
tunnel dials contain zero application-stream bytes rather than charging the later
successful stream to the failed proxy.

Active and manual health checks emit `health_check` events. Their proxy handshake
reads and writes populate only `health_check_bytes`; they do not inflate client,
upstream application-stream, direct, or avoided-byte counters.

## Remaining acceptance work

Load benchmarks, configurable queue/batch sizing, transient-write retry policy and
chunked catch-up remain necessary before production traffic claims. Raw, minute,
hour and day retention policies are independently configured and pruned transactionally.
The legacy `traffic.retention_days` field remains the raw retention policy;
`minute_retention_days`, `hour_retention_days`, and `day_retention_days` control the
coarser tiers. Effective durations are normalized so a coarser tier is never shorter
than the tier below it. Analytics read one non-overlapping authoritative tier for
each time range, and all four watermarks survive restart. Transport-framing
accounting, billing-window/cost budgets and projections are not completed by this
foundation. Configured-cost analytics are estimates over rated application-stream
bytes, not provider invoice reconciliation. Durable hard byte-budget state is
separate from the best-effort analytics queue and fails closed on storage errors.

The dashboard must label estimated avoided bytes as an estimate at every display.
