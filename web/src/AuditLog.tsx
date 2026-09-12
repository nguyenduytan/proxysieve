import { useCallback, useEffect, useState } from "react";
import { RefreshCw, ScrollText } from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type { AuditEvent, AuditPage } from "./api";

export function AuditLog({ onExpired }: { onExpired: () => void }) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState<Date | null>(null);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const page = await api<AuditPage>(
          "/api/v1/audit?limit=100",
          signal ? { signal } : {},
        );
        if (signal?.aborted) return;
        setEvents(page.items);
        setUpdated(new Date());
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

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Audit</h1>
          <p>Review recent administrative activity and change history.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh audit log"
            aria-label="Refresh audit log"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
        </div>
      </header>
      <p className="scope-notice">
        Audit entries contain sanitized metadata only: timestamps, actors,
        actions and target identifiers. Passwords, API tokens, cookies and
        request bodies are never displayed here.
      </p>
      {error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      <section
        className="table-panel audit-panel"
        aria-label="Administrative audit log"
      >
        <div className="section-header">
          <div>
            <h2>Recent activity</h2>
            <span>
              {events.length} loaded
              {loading
                ? " · refreshing…"
                : updated
                  ? ` · updated ${updated.toLocaleTimeString()}`
                  : ""}
            </span>
          </div>
        </div>
        {loading && events.length === 0 ? (
          <div className="empty-state" role="status">
            Loading audit events…
          </div>
        ) : events.length === 0 ? (
          <div className="empty-state">
            <ScrollText size={27} />
            <h3>No audit events yet</h3>
            <p>Administrative changes and sign-in activity will appear here.</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="audit-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Action</th>
                  <th>Target</th>
                  <th>Target ID</th>
                  <th>Actor ID</th>
                  <th>Request ID</th>
                </tr>
              </thead>
              <tbody>
                {events.map((event) => (
                  <tr key={event.id}>
                    <td
                      className="mono muted"
                      data-label="Time"
                      title={event.at}
                    >
                      {formatAuditTime(event.at)}
                    </td>
                    <td data-label="Action">
                      <span className="action-badge audit-action">
                        {event.action}
                      </span>
                    </td>
                    <td data-label="Target">{event.target_type}</td>
                    <td className="mono" data-label="Target ID">
                      {event.target_id || "—"}
                    </td>
                    <td className="mono" data-label="Actor ID">
                      {event.actor_id || "—"}
                    </td>
                    <td className="mono" data-label="Request ID">
                      {event.request_id || "—"}
                    </td>
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

function formatAuditTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "Unknown"
    : date.toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      });
}
