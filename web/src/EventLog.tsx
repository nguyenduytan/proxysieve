import { useCallback, useEffect, useState } from "react";
import { RadioTower, RefreshCw } from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type { EventPage, OperationalEvent } from "./api";

export function mergeOperationalEvents(
  current: OperationalEvent[],
  incoming: OperationalEvent[],
): OperationalEvent[] {
  const byID = new Map(current.map((event) => [event.id, event]));
  for (const event of incoming) byID.set(event.id, event);
  return Array.from(byID.values())
    .sort(
      (left, right) =>
        Date.parse(right.at) - Date.parse(left.at) ||
        right.id.localeCompare(left.id),
    )
    .slice(0, 1000);
}

export function EventLog({ onExpired }: { onExpired: () => void }) {
  const [events, setEvents] = useState<OperationalEvent[]>([]);
  const [dropped, setDropped] = useState(0);
  const [loading, setLoading] = useState(true);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const page = await api<EventPage>("/api/v1/events?limit=1000", {
          ...(signal ? { signal } : {}),
        });
        if (signal?.aborted) return;
        setEvents((current) => mergeOperationalEvents(current, page.items));
        setDropped(page.dropped);
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
    const stream =
      typeof EventSource === "undefined"
        ? null
        : new EventSource("/api/v1/events/stream");
    stream?.addEventListener("open", () => setConnected(true));
    stream?.addEventListener("error", () => setConnected(false));
    stream?.addEventListener("event", (message) => {
      try {
        const event = JSON.parse(
          (message as MessageEvent<string>).data,
        ) as OperationalEvent;
        setEvents((current) => mergeOperationalEvents(current, [event]));
      } catch {
        // Manual refresh remains the recovery path for malformed stream data.
      }
    });
    return () => {
      controller.abort();
      stream?.close();
    };
  }, [load]);

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Events</h1>
          <p>Monitor sanitized control-plane activity as it happens.</p>
        </div>
        <div className="table-actions">
          <span className={connected ? "stream-state live" : "stream-state"}>
            {connected ? "Live" : "Reconnecting"}
          </span>
          <button
            className="icon-button"
            title="Refresh operational events"
            aria-label="Refresh operational events"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
        </div>
      </header>
      <p className="scope-notice">
        Events are sanitized, process-lifetime metadata. Audit remains the
        durable administrator record.{" "}
        {dropped > 0 ? `${dropped} older events were dropped.` : ""}
      </p>
      {error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      <section
        className="table-panel audit-panel"
        aria-label="Operational events"
      >
        <div className="section-header">
          <div>
            <h2>Recent events</h2>
            <span>{events.length} loaded</span>
          </div>
        </div>
        {loading && events.length === 0 ? (
          <div className="empty-state" role="status">
            Loading operational events…
          </div>
        ) : events.length === 0 ? (
          <div className="empty-state">
            <RadioTower size={27} />
            <h3>No operational events yet</h3>
            <p>Control-plane activity will appear here.</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="audit-table event-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Type</th>
                  <th>Severity</th>
                  <th>Source</th>
                  <th>Target</th>
                  <th>Target ID</th>
                  <th>Actor ID</th>
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
                      {formatEventTime(event.at)}
                    </td>
                    <td data-label="Type">
                      <span className="action-badge audit-action">
                        {event.type}
                      </span>
                    </td>
                    <td data-label="Severity">
                      <span className={`event-severity ${event.severity}`}>
                        {event.severity}
                      </span>
                    </td>
                    <td data-label="Source">{event.source}</td>
                    <td data-label="Target">{event.target_type || "—"}</td>
                    <td className="mono" data-label="Target ID">
                      {event.target_id || "—"}
                    </td>
                    <td className="mono" data-label="Actor ID">
                      {event.actor_id || "—"}
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

function formatEventTime(value: string): string {
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
