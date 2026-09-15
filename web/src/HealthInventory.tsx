import { useCallback, useEffect, useState } from "react";
import { Activity, RefreshCw } from "lucide-react";
import {
  ApiError,
  api,
  errorMessage,
  formatBytes,
  type PoolHealth,
  type PoolHealthPage,
  type ProxyHealth,
  type ProxyHealthPage,
  type Role,
} from "./api";

type Feedback = { kind: "error" | "success"; message: string } | null;

export function HealthInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [proxies, setProxies] = useState<ProxyHealth[]>([]);
  const [pools, setPools] = useState<PoolHealth[]>([]);
  const [loading, setLoading] = useState(true);
  const [feedback, setFeedback] = useState<Feedback>(null);
  const [checking, setChecking] = useState("");
  const [targetHost, setTargetHost] = useState("example.com");
  const [targetPort, setTargetPort] = useState(443);
  const mutable = role !== "viewer";
  const targetValid =
    targetHost.trim().length > 0 &&
    targetHost.trim().length <= 253 &&
    Number.isInteger(targetPort) &&
    targetPort >= 1 &&
    targetPort <= 65535;

  const load = useCallback(
    async (signal?: AbortSignal, preserveFeedback = false) => {
      setLoading(true);
      if (!preserveFeedback) setFeedback(null);
      try {
        const [proxyPage, poolPage] = await Promise.all([
          api<ProxyHealthPage>(
            "/api/v1/health/proxies",
            signal ? { signal } : {},
          ),
          api<PoolHealthPage>("/api/v1/health/pools", signal ? { signal } : {}),
        ]);
        setProxies(proxyPage.items);
        setPools(poolPage.items);
      } catch (caught) {
        if (caught instanceof ApiError && caught.status === 401) onExpired();
        else if (!(
          caught instanceof DOMException && caught.name === "AbortError"
        ))
          setFeedback({ kind: "error", message: errorMessage(caught) });
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [onExpired],
  );

  async function check(kind: "proxies" | "pools", id: string, name: string) {
    const key = `${kind}:${id}`;
    setChecking(key);
    setFeedback(null);
    try {
      await api(`/api/v1/health/${kind}/${encodeURIComponent(id)}/check`, {
        method: "POST",
        body: { target_host: targetHost.trim(), target_port: targetPort },
        timeoutMs: 35_000,
      });
      setFeedback({
        kind: "success",
        message: `${name} health check completed.`,
      });
      await load(undefined, true);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setFeedback({ kind: "error", message: errorMessage(caught) });
    } finally {
      setChecking("");
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  return (
    <div className="content">
      <header className="page-heading">
        <div>
          <h1>Health</h1>
        </div>
        <button
          className="pause-button secondary"
          disabled={loading}
          onClick={() => void load()}
        >
          <RefreshCw size={15} />
          Refresh
        </button>
      </header>
      {mutable ? (
        <section
          className="resource-form"
          aria-labelledby="health-target-title"
        >
          <h2 id="health-target-title">Manual check target</h2>
          <div className="form-grid">
            <label>
              Host
              <input
                value={targetHost}
                maxLength={253}
                required
                onChange={(event) => setTargetHost(event.target.value)}
              />
            </label>
            <label>
              Port
              <input
                type="number"
                min={1}
                max={65535}
                required
                value={targetPort}
                onChange={(event) => setTargetPort(Number(event.target.value))}
              />
            </label>
          </div>
        </section>
      ) : null}
      {feedback ? (
        <p
          role={feedback.kind === "error" ? "alert" : "status"}
          className={
            feedback.kind === "error" ? "auth-error" : "success-notice"
          }
        >
          {feedback.message}
        </p>
      ) : null}
      <section className="table-panel">
        <div className="section-header">
          <h2>Pool availability</h2>
        </div>
        {loading && pools.length === 0 ? (
          <div className="empty-state">Loading pool health…</div>
        ) : pools.length === 0 ? (
          <div className="empty-state">No active pools</div>
        ) : (
          <div className="table-scroll">
            <table className="pool-inventory-table">
              <thead>
                <tr>
                  <th>Pool</th>
                  <th>Status</th>
                  <th>Eligible</th>
                  <th>Healthy</th>
                  <th>Degraded</th>
                  <th>Quarantined</th>
                  <th>Unknown</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {pools.map((pool) => (
                  <tr key={pool.pool_id}>
                    <td className="pool-name-cell" data-label="Pool">
                      <strong>{pool.name}</strong>
                      <span>{pool.pool_id}</span>
                    </td>
                    <td data-label="Status">
                      <span
                        className={`client-state ${pool.enabled ? "enabled" : "disabled"}`}
                      >
                        {pool.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </td>
                    <td data-label="Eligible">
                      {pool.eligible} / {pool.total}
                    </td>
                    <td data-label="Healthy">{pool.healthy}</td>
                    <td data-label="Degraded">{pool.degraded}</td>
                    <td data-label="Quarantined">
                      {pool.quarantined + pool.half_open}
                    </td>
                    <td data-label="Unknown">{pool.unknown}</td>
                    {mutable ? (
                      <td className="pool-action-cell">
                        <div className="row-actions">
                          <button
                            className="icon-button pool-action-button"
                            title="Check pool health"
                            aria-label={`Check ${pool.name} health`}
                            disabled={
                              !pool.enabled ||
                              !targetValid ||
                              checking !== "" ||
                              loading
                            }
                            onClick={() =>
                              void check("pools", pool.pool_id, pool.name)
                            }
                          >
                            <Activity size={14} />
                            <span>
                              {checking === `pools:${pool.pool_id}`
                                ? "Checking…"
                                : "Check"}
                            </span>
                          </button>
                        </div>
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <section className="table-panel">
        <div className="section-header">
          <h2>Proxy health</h2>
        </div>
        {loading && proxies.length === 0 ? (
          <div className="empty-state">Loading proxy health…</div>
        ) : proxies.length === 0 ? (
          <div className="empty-state">No active proxies</div>
        ) : (
          <div className="table-scroll">
            <table className="pool-inventory-table">
              <thead>
                <tr>
                  <th>Proxy</th>
                  <th>State</th>
                  <th>Circuit</th>
                  <th>Score</th>
                  <th>Latency</th>
                  <th>Success</th>
                  <th>Failures</th>
                  <th>Last result</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {proxies.map((proxy) => (
                  <tr key={proxy.proxy_id}>
                    <td className="pool-name-cell" data-label="Proxy">
                      <strong>{proxy.name}</strong>
                      <span>{proxy.proxy_id}</span>
                    </td>
                    <td data-label="State">
                      <span
                        className={`client-state ${stateClass(proxy.state)}`}
                      >
                        {proxy.state.replace("_", " ")}
                      </span>
                    </td>
                    <td data-label="Circuit">
                      {proxy.circuit.replace("_", " ")}
                    </td>
                    <td data-label="Score">{proxy.score}</td>
                    <td data-label="Latency">
                      <div className="health-signal">
                        <strong>{formatLatency(proxy.latency_ns)}</strong>
                        <span>{formatTimingSignals(proxy)}</span>
                      </div>
                    </td>
                    <td data-label="Success">
                      <div className="health-signal">
                        <strong>{formatSuccessRate(proxy)}</strong>
                        <span>{formatThroughput(proxy)}</span>
                      </div>
                    </td>
                    <td data-label="Failures">
                      <div className="health-signal">
                        <strong>{proxy.consecutive_failures} streak</strong>
                        <span>{formatFailureSignals(proxy)}</span>
                      </div>
                    </td>
                    <td data-label="Last result">{formatLastResult(proxy)}</td>
                    {mutable ? (
                      <td className="pool-action-cell">
                        <div className="row-actions">
                          <button
                            className="icon-button pool-action-button"
                            title="Check proxy health"
                            aria-label={`Check ${proxy.name} health`}
                            disabled={
                              proxy.state === "disabled" ||
                              !targetValid ||
                              checking !== "" ||
                              loading
                            }
                            onClick={() =>
                              void check("proxies", proxy.proxy_id, proxy.name)
                            }
                          >
                            <Activity size={14} />
                            <span>
                              {checking === `proxies:${proxy.proxy_id}`
                                ? "Checking…"
                                : "Check"}
                            </span>
                          </button>
                        </div>
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function stateClass(state: ProxyHealth["state"]): string {
  if (state === "healthy") return "enabled";
  if (state === "unknown" || state === "degraded" || state === "half_open")
    return "staged";
  return "disabled";
}

function formatLatency(nanoseconds: number): string {
  if (!Number.isFinite(nanoseconds) || nanoseconds <= 0) return "—";
  const milliseconds = nanoseconds / 1_000_000;
  return `${milliseconds.toFixed(milliseconds < 10 ? 1 : 0)} ms`;
}

export function formatSuccessRate(proxy: ProxyHealth): string {
  if (proxy.observations <= 0) return "—";
  return `${Math.round((proxy.successes / proxy.observations) * 100)}% (${proxy.successes}/${proxy.observations})`;
}

export function formatTimingSignals(proxy: ProxyHealth): string {
  return `Connect ${formatLatency(proxy.connect_latency_ns)} · TTFB ${formatLatency(proxy.ttfb_ns)}`;
}

export function formatThroughput(proxy: ProxyHealth): string {
  return proxy.throughput_bytes_per_sec > 0
    ? `${formatBytes(proxy.throughput_bytes_per_sec)}/s`
    : "—";
}

export function formatFailureSignals(proxy: ProxyHealth): string {
  if (proxy.observations <= 0) return "No observations";
  const signals = (
    [
      ["timeout", proxy.timeouts],
      ["auth", proxy.auth_failures],
      ["DNS", proxy.dns_failures],
      ["TLS", proxy.tls_failures],
      ["403", proxy.status_403],
      ["407", proxy.status_407],
      ["429", proxy.status_429],
      ["5xx", proxy.status_5xx],
    ] as [string, number][]
  )
    .filter(([, count]) => count > 0)
    .map(([label, count]) => `${label} ${count}`);
  return `${proxy.failures}/${proxy.observations} recent${signals.length ? ` · ${signals.join(" · ")}` : ""}`;
}

export function formatLastResult(proxy: ProxyHealth): string {
  const parse = (raw?: string) => {
    if (!raw) return null;
    const date = new Date(raw);
    return Number.isNaN(date.getTime()) ? null : date;
  };
  const success = parse(proxy.last_success);
  const failure = parse(proxy.last_failure);
  const successTime = success?.getTime() ?? Number.NEGATIVE_INFINITY;
  const failureTime = failure?.getTime() ?? Number.NEGATIVE_INFINITY;
  const latest = failureTime > successTime ? failure : success || failure;
  if (!latest)
    return proxy.last_failure || proxy.last_success ? "Unknown" : "Never";
  return latest.toLocaleString();
}
