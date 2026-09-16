import { useMemo, useState } from "react";
import {
  Activity,
  ArrowDownToLine,
  BarChart3,
  CircleDollarSign,
  Database,
  Gauge,
  Pause,
  Play,
  Search,
  ShieldCheck,
  TrendingUp,
} from "lucide-react";
import { formatBytes, formatConfiguredCosts } from "./api";
import type {
  TrafficBreakdown,
  TrafficEvent,
  TrafficPage,
  TrafficSummary,
} from "./api";

const hourMilliseconds = 60 * 60 * 1000;
const thirtyDaysMilliseconds = 30 * 24 * hourMilliseconds;

function thirtyDayProjectionFactor(
  summary: TrafficSummary | undefined,
  now: number,
) {
  if (!summary) return null;
  const from = Date.parse(summary.from);
  const until = Math.min(Date.parse(summary.until), now);
  if (!Number.isFinite(from) || !Number.isFinite(until) || until <= from)
    return null;
  return thirtyDaysMilliseconds / (until - from);
}

export function projectThirtyDayPaidBytes(
  summary: TrafficSummary | undefined,
  now = Date.now(),
) {
  const factor = thirtyDayProjectionFactor(summary, now);
  if (!summary || factor === null) return null;
  const bytes =
    summary.totals.upstream_upload_bytes +
    summary.totals.upstream_download_bytes;
  if (!Number.isSafeInteger(bytes) || bytes < 0) return null;
  const projected = Math.round(bytes * factor);
  return Number.isSafeInteger(projected) ? projected : null;
}

export function projectThirtyDayConfiguredCosts(
  summary: TrafficSummary | undefined,
  now = Date.now(),
) {
  const factor = thirtyDayProjectionFactor(summary, now);
  if (!summary?.configured_costs?.length || factor === null) return undefined;
  const projected = summary.configured_costs.map((cost) => ({
    ...cost,
    amount: {
      ...cost.amount,
      micros: Math.round(cost.amount.micros * factor),
    },
  }));
  return projected.every(
    ({ amount }) => Number.isSafeInteger(amount.micros) && amount.micros >= 0,
  )
    ? projected
    : undefined;
}

export function fillHourlySeries(data: TrafficPage["series"]) {
  if (!data || data.granularity !== "hour") return [];
  const from = Date.parse(data.from);
  const until = Date.parse(data.until);
  if (
    !Number.isFinite(from) ||
    !Number.isFinite(until) ||
    until <= from ||
    (until - from) / hourMilliseconds > 2000
  )
    return [];
  const counts = new Map(
    data.points.map((point) => [
      Date.parse(point.bucket_start),
      point.totals.request_count,
    ]),
  );
  const buckets = [];
  for (let at = from; at < until; at += hourMilliseconds) {
    buckets.push({ at, count: counts.get(at) ?? 0 });
  }
  return buckets;
}

export function TrafficView({
  overview,
  data,
  error,
  paused,
  updated,
  onToggle,
}: {
  overview: boolean;
  data: TrafficPage | null;
  error: string;
  paused: boolean;
  updated: Date | null;
  onToggle: () => void;
}) {
  const [filter, setFilter] = useState("");
  const rows = useMemo(
    () =>
      (data?.events ?? []).filter((row) =>
        `${row.host} ${row.protocol} ${row.action} ${row.pool_id} ${row.proxy_id} ${row.chain_id} ${row.policy_id} ${row.rule_id}`
          .toLowerCase()
          .includes(filter.toLowerCase()),
      ),
    [data, filter],
  );
  const totals = data?.summary?.totals;
  const upstream = totals
    ? totals.upstream_upload_bytes + totals.upstream_download_bytes
    : null;
  const direct = totals?.direct_bytes ?? null;
  const projectedPaid = projectThirtyDayPaidBytes(data?.summary);
  const projectedCosts = projectThirtyDayConfiguredCosts(data?.summary);
  const hourly = useMemo(() => fillHourlySeries(data?.series), [data?.series]);
  const persistenceLosses =
    (data?.durable?.queue_dropped ?? 0) + (data?.durable?.failed_events ?? 0);
  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>{overview ? "Overview" : "Traffic"}</h1>
          <p>Application-stream measurements. Not provider-billed bytes.</p>
        </div>
        <span className="live-signal">
          <span className={paused || error ? "pulse paused" : "pulse"} />
          {paused
            ? "Paused"
            : error
              ? "Connection interrupted"
              : "Live · 5s fallback"}
        </span>
      </header>
      <div className="scope-notice">
        Local development build · Live and retained HTTP, CONNECT and SOCKS5
        events with bounded summary and minute/hour/day rollups. Transport
        framing plus soft and cost budgets are not available yet. Configured
        costs and projections are estimates; configured hard byte budgets are
        enforced in the gateway.
      </div>
      {error && (
        <div role="alert" className="auth-error">
          {error} Last received data is shown below.
        </div>
      )}
      {overview && (
        <section className="metric-grid">
          <DataMetric
            icon={<ArrowDownToLine size={17} />}
            label="Paid proxy stream bytes"
            value={upstream === null ? "—" : formatBytes(upstream)}
            context="Last 24 hours"
          />
          <DataMetric
            icon={<ShieldCheck size={17} />}
            label="Direct stream bytes"
            value={direct === null ? "—" : formatBytes(direct)}
            context="Last 24 hours"
          />
          <DataMetric
            icon={<Activity size={17} />}
            label="Events"
            value={totals ? String(totals.request_count) : "—"}
            context={
              data
                ? `Last 24 hours · ${data.dropped} live evictions`
                : "24-hour summary unavailable"
            }
          />
          <DataMetric
            icon={<CircleDollarSign size={17} />}
            label="Configured estimated cost"
            value={formatConfiguredCosts(data?.summary?.configured_costs)}
            context="Last 24 hours · rated proxy routes"
          />
          <DataMetric
            icon={<TrendingUp size={17} />}
            label="30-day paid traffic projection"
            value={projectedPaid === null ? "—" : formatBytes(projectedPaid)}
            context={
              projectedCosts?.length
                ? `Estimate · ${formatConfiguredCosts(projectedCosts)} configured cost`
                : "Estimate from the current 24-hour rate"
            }
          />
          <DataMetric
            icon={<Activity size={17} />}
            label="Events not persisted"
            value={data ? String(persistenceLosses) : "—"}
            context={
              data?.durable
                ? `${data.durable.queued} queued · ${data.durable.write_failures} failed batches`
                : "Persistence status unavailable"
            }
          />
        </section>
      )}
      {overview && (
        <TrafficChart buckets={hourly} total={totals?.request_count ?? null} />
      )}
      {!overview && (
        <section
          className="metric-grid savings-grid"
          aria-label="Saved traffic measurement"
        >
          <DataMetric
            icon={<Database size={17} />}
            label="Exact cache savings"
            value={totals ? formatBytes(totals.cache_served_bytes) : "—"}
            context="Bytes served from ProxySieve cache · last 24 hours"
          />
          <DataMetric
            icon={<Gauge size={17} />}
            label="Estimated avoided traffic"
            value={
              totals?.estimated_avoided_bytes
                ? formatBytes(totals.estimated_avoided_bytes)
                : "—"
            }
            context={
              totals?.estimated_avoided_bytes
                ? "Evidence-based estimate · last 24 hours"
                : "No evidence-backed estimates recorded"
            }
          />
        </section>
      )}
      {!overview && (
        <section
          className="traffic-breakdowns"
          aria-label="Traffic attribution"
        >
          <BreakdownList
            title="Paid traffic by pool"
            data={data?.pool_breakdown}
            value={(item) =>
              formatBytes(
                item.totals.upstream_upload_bytes +
                  item.totals.upstream_download_bytes,
              )
            }
          />
          <BreakdownList
            title="Blocked requests by rule"
            data={data?.blocked_rule_breakdown}
            value={(item) => eventCountLabel(item.totals.request_count)}
          />
        </section>
      )}
      <section className="table-panel" aria-label="Recorded gateway traffic">
        <div className="section-header">
          <div>
            <h2>Recorded gateway traffic</h2>
            <span>
              {updated
                ? `Last refreshed ${updated.toLocaleTimeString()}`
                : "Waiting for the control plane"}
            </span>
          </div>
          <div className="table-actions">
            <label className="search-box">
              <Search size={15} />
              <input
                aria-label="Search live traffic"
                placeholder="Host, action, policy, rule…"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
              />
            </label>
            <button
              className="pause-button"
              onClick={onToggle}
              aria-pressed={paused}
            >
              {paused ? <Play size={14} /> : <Pause size={14} />}{" "}
              {paused ? "Resume" : "Pause"}
            </button>
          </div>
        </div>
        {data === null ? (
          <div className="empty-state" role="status">
            Loading gateway events…
          </div>
        ) : rows.length === 0 ? (
          <div className="empty-state">
            <Activity size={27} />
            <h3>{filter ? "No matching events" : "No gateway events yet"}</h3>
            <p>
              {filter
                ? "Try another host, protocol, action, policy or rule."
                : "Configure an explicit route and send HTTP, CONNECT or SOCKS5 traffic through the gateway."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Protocol</th>
                  <th>Host</th>
                  <th>Policy</th>
                  <th>Rule</th>
                  <th>Pool</th>
                  <th>Chain</th>
                  <th>Proxy</th>
                  <th>Status</th>
                  <th>Paid proxy bytes</th>
                  <th>Direct bytes</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <TrafficLine
                    key={`${row.request_id}:${row.connection_id}:${row.at}`}
                    row={row}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function TrafficChart({
  buckets,
  total,
}: {
  buckets: { at: number; count: number }[];
  total: number | null;
}) {
  const maximum = Math.max(1, ...buckets.map((bucket) => bucket.count));
  const eventTotal = total === null ? "—" : eventCountLabel(total);
  return (
    <section
      className="chart-panel traffic-chart"
      aria-label="Gateway event activity"
    >
      <header>
        <div>
          <h2>Gateway event activity</h2>
          <span>Hourly totals from the bounded analytics API</span>
        </div>
        <b>{eventTotal}</b>
      </header>
      {buckets.length === 0 ? (
        <div className="chart-empty" role="status">
          Waiting for hourly analytics…
        </div>
      ) : (
        <div
          className="hour-bars"
          role="img"
          aria-label={`Hourly gateway event activity, ${eventCountLabel(total ?? 0)} in the last 24 hours`}
        >
          {buckets.map((bucket) => (
            <span
              key={bucket.at}
              title={`${new Date(bucket.at).toLocaleString()}: ${eventCountLabel(bucket.count)}`}
              style={{
                height: `${Math.max(bucket.count ? 8 : 2, (bucket.count / maximum) * 100)}%`,
              }}
            />
          ))}
        </div>
      )}
      <footer>
        <span>
          {buckets[0]
            ? new Date(buckets[0].at).toLocaleTimeString([], {
                hour: "2-digit",
              })
            : "24h ago"}
        </span>
        <BarChart3 size={14} aria-hidden="true" />
        <span>Now</span>
      </footer>
    </section>
  );
}

function BreakdownList({
  title,
  data,
  value,
}: {
  title: string;
  data: TrafficBreakdown | undefined;
  value: (item: TrafficBreakdown["items"][number]) => string;
}) {
  return (
    <div>
      <h2>{title}</h2>
      {!data ? (
        <p className="muted">Loading…</p>
      ) : data.items.length === 0 ? (
        <p className="muted">No attributed traffic</p>
      ) : (
        <ol>
          {data.items.map((item) => (
            <li key={item.value}>
              <span className="mono">{item.value}</span>
              <strong>{value(item)}</strong>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function eventCountLabel(count: number) {
  return `${count} ${count === 1 ? "event" : "events"}`;
}
function DataMetric({
  icon,
  label,
  value,
  context = "This process · retained events",
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  context?: string;
}) {
  return (
    <article className="metric">
      <div className="metric-icon blue">{icon}</div>
      <div>
        <p>{label}</p>
        <strong>{value}</strong>
        <span className="metric-change neutral">{context}</span>
      </div>
    </article>
  );
}
function TrafficLine({ row }: { row: TrafficEvent }) {
  const timestamp = new Date(row.at);
  return (
    <tr>
      <td className="mono muted" title={timestamp.toISOString()}>
        {timestamp.toLocaleString([], {
          month: "short",
          day: "numeric",
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })}
      </td>
      <td>
        <span className={`action-badge ${row.action}`}>
          {row.action.toUpperCase()}
        </span>
      </td>
      <td className="mono">{row.protocol.toUpperCase()}</td>
      <td className="host-cell">{row.host}</td>
      <td>{row.policy_id || "—"}</td>
      <td>{row.rule_id || "—"}</td>
      <td>{row.pool_id || "—"}</td>
      <td>{row.chain_id || "—"}</td>
      <td>{row.proxy_id || "—"}</td>
      <td>{trafficStatus(row)}</td>
      <td>
        {formatBytes(row.upstream_upload_bytes + row.upstream_download_bytes)}
      </td>
      <td>{formatBytes(row.direct_bytes)}</td>
    </tr>
  );
}

function trafficStatus(row: TrafficEvent) {
  if (row.protocol === "socks5") {
    return row.status_code === 0 ? "OK" : `SOCKS ${row.status_code}`;
  }
  return row.status_code || "—";
}
