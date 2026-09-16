import { useCallback, useEffect, useState } from "react";
import { Database, RefreshCw, Trash2, X } from "lucide-react";
import {
  ApiError,
  api,
  errorMessage,
  formatBytes,
  type CachePurgeResult,
  type CacheStatus,
  type Role,
} from "./api";

export function CacheInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [status, setStatus] = useState<CacheStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [purging, setPurging] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [domain, setDomain] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        setStatus(
          await api<CacheStatus>(
            "/api/v1/cache/stats",
            signal ? { signal } : {},
          ),
        );
      } catch (caught) {
        if (caught instanceof ApiError && caught.status === 401) onExpired();
        else if (!(
          caught instanceof DOMException && caught.name === "AbortError"
        ))
          setError(errorMessage(caught));
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [onExpired],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function purge(targetDomain = "") {
    setPurging(true);
    setError("");
    setMessage("");
    try {
      const result = await api<CachePurgeResult>(
        targetDomain ? "/api/v1/cache/purge/domain" : "/api/v1/cache/purge",
        {
          method: "POST",
          ...(targetDomain ? { body: { domain: targetDomain } } : {}),
        },
      );
      setStatus({
        enabled: true,
        storage: result.storage,
        stats: result.stats,
      });
      setMessage(
        `Purged ${result.purged.entries} entries${result.domain ? ` for ${result.domain}` : ""} (${formatBytes(result.purged.bytes)}).`,
      );
      if (targetDomain) setDomain("");
      setConfirming(false);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setPurging(false);
    }
  }

  const stats = status?.stats;
  const canPurge =
    role !== "viewer" && status?.enabled === true && stats !== undefined;
  return (
    <div className="content">
      <header className="page-heading">
        <div>
          <h1>Cache</h1>
        </div>
        <button
          className="pause-button secondary"
          disabled={loading || purging}
          onClick={() => void load()}
        >
          <RefreshCw size={15} />
          Refresh
        </button>
      </header>
      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
      {message ? (
        <div role="status" className="success-notice">
          {message}
        </div>
      ) : null}
      {confirming ? (
        <div className="inline-confirm runtime-confirm" role="group">
          <span>Purge every cached response?</span>
          <div>
            <button
              className="danger-button"
              disabled={purging}
              onClick={() => void purge()}
            >
              <Trash2 size={14} />
              {purging ? "Purging…" : "Confirm purge"}
            </button>
            <button
              className="icon-button"
              aria-label="Cancel cache purge"
              disabled={purging}
              onClick={() => setConfirming(false)}
            >
              <X size={15} />
            </button>
          </div>
        </div>
      ) : null}
      {loading && !status ? (
        <section className="table-panel">
          <div className="empty-state">Loading cache status…</div>
        </section>
      ) : !status?.enabled || !stats ? (
        <section className="table-panel">
          <div className="empty-state">
            <Database size={27} />
            <h3>Response cache disabled</h3>
          </div>
        </section>
      ) : (
        <>
          <section className="metric-grid" aria-label="Cache statistics">
            <CacheMetric
              label="Stored"
              value={formatBytes(stats.bytes_stored)}
              context={`${status.storage === "disk" ? "Disk" : "Memory"} · ${stats.entries} of ${stats.max_entries} entries`}
            />
            <CacheMetric
              label="Capacity"
              value={formatBytes(stats.max_bytes)}
              context={`${formatPercent(stats.bytes_stored / stats.max_bytes)} used`}
            />
            <CacheMetric
              label="Hit ratio"
              value={formatPercent(stats.hit_ratio)}
              context={`${stats.hits} hits · ${stats.misses} misses`}
            />
            <CacheMetric
              label="Bytes served"
              value={formatBytes(stats.bytes_served)}
              context="Delivered from response cache"
            />
            <CacheMetric
              label="Bypasses"
              value={stats.bypasses.toLocaleString()}
              context="Ineligible requests"
            />
            <CacheMetric
              label="Removed"
              value={(stats.expired + stats.evictions).toLocaleString()}
              context={`${stats.expired} expired · ${stats.evictions} evicted`}
            />
          </section>
          {canPurge && !confirming ? (
            <div className="cache-actions">
              <form
                onSubmit={(event) => {
                  event.preventDefault();
                  setMessage("");
                  void purge(domain.trim());
                }}
              >
                <label htmlFor="cache-domain">Exact hostname</label>
                <div>
                  <input
                    id="cache-domain"
                    value={domain}
                    onChange={(event) => setDomain(event.target.value)}
                    placeholder="static.example.com"
                    required
                    maxLength={253}
                  />
                  <button
                    className="pause-button secondary"
                    disabled={purging || !domain.trim()}
                    type="submit"
                  >
                    <Trash2 size={14} />
                    Purge hostname
                  </button>
                </div>
              </form>
              <button
                className="danger-button"
                onClick={() => {
                  setMessage("");
                  setConfirming(true);
                }}
              >
                <Trash2 size={14} />
                Purge all
              </button>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}

function CacheMetric({
  label,
  value,
  context,
}: {
  label: string;
  value: string;
  context: string;
}) {
  return (
    <article className="metric">
      <div>
        <p>{label}</p>
        <strong>{value}</strong>
        <span className="metric-change neutral">{context}</span>
      </div>
    </article>
  );
}

export function formatPercent(value: number): string {
  return Number.isFinite(value) && value >= 0
    ? `${(value * 100).toFixed(1)}%`
    : "—";
}
