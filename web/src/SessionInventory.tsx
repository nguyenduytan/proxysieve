import { useCallback, useEffect, useState } from "react";
import { Fingerprint, RefreshCw, RotateCcw, Trash2, X } from "lucide-react";
import { ApiError, api, errorMessage, formatBytes } from "./api";
import type { ProxySession, Role, SessionPage } from "./api";

export function SessionInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [items, setItems] = useState<ProxySession[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [confirming, setConfirming] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const mutable = role !== "viewer";

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      setNotice("");
      try {
        const response = await api<SessionPage>("/api/v1/sessions", {
          ...(signal ? { signal } : {}),
        });
        if (!signal?.aborted) setItems(response.items ?? []);
      } catch (caught) {
        if (signal?.aborted) return;
        if (caught instanceof ApiError && caught.status === 401) onExpired();
        else setError(errorMessage(caught));
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

  async function rotate(item: ProxySession) {
    setBusy(item.id);
    setError("");
    setNotice("");
    try {
      const updated = await api<ProxySession>(
        `/api/v1/sessions/${encodeURIComponent(item.id)}/rotate`,
        { method: "POST", body: {} },
      );
      setItems((current) =>
        current.map((candidate) =>
          candidate.id === updated.id ? updated : candidate,
        ),
      );
      setNotice("Session rotated. Its next request will select a new proxy.");
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  async function remove(item: ProxySession) {
    setBusy(item.id);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/sessions/${encodeURIComponent(item.id)}`, {
        method: "DELETE",
        body: {},
      });
      setItems((current) =>
        current.filter((candidate) => candidate.id !== item.id),
      );
      setConfirming("");
      setNotice(
        "Session removed. A matching request will create a new binding.",
      );
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Sessions</h1>
          <p>Inspect active proxy affinity and rotate subsequent requests.</p>
        </div>
        <button
          className="icon-button"
          title="Refresh sessions"
          aria-label="Refresh sessions"
          disabled={loading}
          onClick={() => void load()}
        >
          <RefreshCw size={17} />
        </button>
      </header>
      <p className="scope-notice">
        Runtime sessions persist with the local SQLite control plane. Raw
        affinity keys are never stored or displayed. Rotation applies to the
        next request, never interrupts an active tunnel, and is re-evaluated
        after runtime changes.
      </p>
      {error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : notice ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      <section className="table-panel" aria-label="Runtime proxy sessions">
        <div className="section-header">
          <div>
            <h2>Runtime sessions</h2>
            <span>
              {items.length} loaded ·{" "}
              {mutable ? "operator controls" : "read only"}
            </span>
          </div>
        </div>
        {loading && items.length === 0 ? (
          <div className="empty-state" role="status">
            Loading sessions…
          </div>
        ) : items.length === 0 ? (
          <div className="empty-state">
            <Fingerprint size={27} />
            <h3>No runtime sessions</h3>
            <p>
              Sticky bindings appear after traffic uses a session-enabled pool.
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="session-inventory-table">
              <thead>
                <tr>
                  <th>Session</th>
                  <th>Client</th>
                  <th>Route</th>
                  <th>Usage</th>
                  <th>Lifetime</th>
                  <th>Status</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {items.map((item) => {
                  const working = busy === item.id;
                  return (
                    <tr key={item.id}>
                      <td className="session-name-cell" data-label="Session">
                        <strong>{shortHash(item.key_hash)}</strong>
                        <span>{item.policy.strategy}</span>
                      </td>
                      <td data-label="Client">{item.client_id}</td>
                      <td data-label="Route">
                        <span className="session-cell-stack">
                          <strong>{item.pool_id}</strong>
                          <span className="mono">{item.proxy_endpoint_id}</span>
                        </span>
                      </td>
                      <td data-label="Usage">
                        {item.request_count} requests ·{" "}
                        {formatBytes(item.upload_bytes + item.download_bytes)}
                      </td>
                      <td data-label="Lifetime">
                        {formatAge(item.created_at)} old · expires{" "}
                        {formatRemaining(item.expires_at)} · idle expires{" "}
                        {formatRemaining(item.idle_expires_at)}
                      </td>
                      <td data-label="Status">
                        <span className="session-cell-stack">
                          <span
                            className={`client-state ${item.status === "active" ? "enabled" : "disabled"}`}
                          >
                            {item.status}
                          </span>
                          <span>{formatReason(item.rotation_reason)}</span>
                        </span>
                      </td>
                      {mutable ? (
                        <td className="session-action-cell">
                          {confirming === item.id ? (
                            <div
                              className="inline-confirm"
                              role="group"
                              aria-label="Delete session"
                            >
                              <button
                                className="danger-button"
                                disabled={working}
                                onClick={() => void remove(item)}
                              >
                                <Trash2 size={14} />
                                {working ? "Deleting…" : "Confirm delete"}
                              </button>
                              <button
                                className="icon-button"
                                aria-label="Cancel deleting session"
                                disabled={working}
                                onClick={() => setConfirming("")}
                              >
                                <X size={14} />
                              </button>
                            </div>
                          ) : (
                            <div className="row-actions">
                              <button
                                className="icon-button session-action-button"
                                title="Rotate session"
                                aria-label={`Rotate session ${item.id}`}
                                disabled={working || item.status !== "active"}
                                onClick={() => void rotate(item)}
                              >
                                <RotateCcw size={14} />
                                <span>{working ? "Rotating…" : "Rotate"}</span>
                              </button>
                              <button
                                className="icon-button session-action-button danger-icon"
                                title="Delete session"
                                aria-label={`Delete session ${item.id}`}
                                disabled={working}
                                onClick={() => setConfirming(item.id)}
                              >
                                <Trash2 size={14} />
                                <span>Delete</span>
                              </button>
                            </div>
                          )}
                        </td>
                      ) : null}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function shortHash(value: string): string {
  return value ? `${value.slice(0, 12)}…` : "Untracked";
}

function formatAge(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "unknown";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86_400)}d`;
}

function formatReason(value: string): string {
  return value.replaceAll("_", " ");
}

function formatRemaining(value: string): string {
  if (!value || value.startsWith("0001-")) return "off";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "unknown";
  const seconds = Math.floor((timestamp - Date.now()) / 1000);
  if (seconds <= 0) return "expired";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86_400)}d`;
}
