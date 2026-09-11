import { useMemo, useState } from "react";
import {
  Activity,
  ArrowDownToLine,
  Pause,
  Play,
  Search,
  ShieldCheck,
} from "lucide-react";
import { formatBytes } from "./api";
import type { TrafficEvent, TrafficPage } from "./api";

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
      (data?.events ?? [])
        .filter((row) =>
          `${row.host} ${row.action} ${row.pool_id} ${row.proxy_id}`
            .toLowerCase()
            .includes(filter.toLowerCase()),
        )
        .slice()
        .reverse(),
    [data, filter],
  );
  const events = data?.events ?? [];
  const upstream = events.reduce(
    (sum, event) =>
      sum + event.upstream_upload_bytes + event.upstream_download_bytes,
    0,
  );
  const direct = events.reduce((sum, event) => sum + event.direct_bytes, 0);
  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>{overview ? "Overview" : "Live traffic"}</h1>
          <p>
            HTTP body measurements from this process. Not provider-billed bytes.
          </p>
        </div>
        <span className="live-signal">
          <span className={paused || error ? "pulse paused" : "pulse"} />
          {paused
            ? "Paused"
            : error
              ? "Connection interrupted"
              : "Refreshing every 5s"}
        </span>
      </header>
      <div className="scope-notice">
        Local development build · Retained HTTP events only. Tunnel totals,
        historical analytics, budgets and estimated savings are not available
        yet.
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
            label="Proxy response/request bodies"
            value={data ? formatBytes(upstream) : "—"}
          />
          <DataMetric
            icon={<ShieldCheck size={17} />}
            label="Direct body bytes"
            value={data ? formatBytes(direct) : "—"}
          />
          <DataMetric
            icon={<Activity size={17} />}
            label="Retained request events"
            value={data ? String(events.length) : "—"}
          />
          <DataMetric
            icon={<Activity size={17} />}
            label="Events dropped at capacity"
            value={data ? String(data.dropped) : "—"}
          />
        </section>
      )}
      <section className="table-panel" aria-label="Recorded HTTP traffic">
        <div className="section-header">
          <div>
            <h2>Recorded HTTP traffic</h2>
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
                placeholder="Host, action, pool…"
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
            Loading request events…
          </div>
        ) : rows.length === 0 ? (
          <div className="empty-state">
            <Activity size={27} />
            <h3>
              {filter ? "No matching requests" : "No HTTP request events yet"}
            </h3>
            <p>
              {filter
                ? "Try another host, action or pool."
                : "Configure an explicit route and send HTTP traffic through the gateway. HTTPS/SOCKS tunnels are not included in this view yet."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Host</th>
                  <th>Pool</th>
                  <th>Proxy</th>
                  <th>Status</th>
                  <th>Proxy body bytes</th>
                  <th>Direct body bytes</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <TrafficLine key={`${row.request_id}-${index}`} row={row} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
function DataMetric({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
}) {
  return (
    <article className="metric">
      <div className="metric-icon blue">{icon}</div>
      <div>
        <p>{label}</p>
        <strong>{value}</strong>
        <span className="metric-change neutral">
          This process · retained events
        </span>
      </div>
    </article>
  );
}
function TrafficLine({ row }: { row: TrafficEvent }) {
  return (
    <tr>
      <td className="mono muted">{new Date(row.at).toLocaleTimeString()}</td>
      <td>
        <span className={`action-badge ${row.action}`}>
          {row.action.toUpperCase()}
        </span>
      </td>
      <td className="host-cell">{row.host}</td>
      <td>{row.pool_id || "—"}</td>
      <td>{row.proxy_id || "—"}</td>
      <td>{row.status_code || "—"}</td>
      <td>
        {formatBytes(row.upstream_upload_bytes + row.upstream_download_bytes)}
      </td>
      <td>{formatBytes(row.direct_bytes)}</td>
    </tr>
  );
}
