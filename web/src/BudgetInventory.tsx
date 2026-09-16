import { useCallback, useEffect, useState } from "react";
import { Gauge, RefreshCw } from "lucide-react";
import {
  ApiError,
  api,
  errorMessage,
  formatBytes,
  type BudgetStatus,
  type BudgetStatusPage,
} from "./api";

export function BudgetInventory({ onExpired }: { onExpired: () => void }) {
  const [items, setItems] = useState<BudgetStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const page = await api<BudgetStatusPage>(
          "/api/v1/budgets",
          signal ? { signal } : {},
        );
        setItems(page.items);
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

  return (
    <div className="content">
      <header className="page-heading">
        <div>
          <h1>Budgets</h1>
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
      {error ? (
        <p className="auth-error" role="alert">
          {error}
        </p>
      ) : null}
      <section className="table-panel" aria-label="Active budget usage">
        <div className="section-header">
          <h2>Active limits</h2>
        </div>
        {loading && items.length === 0 ? (
          <div className="empty-state">Loading budget usage…</div>
        ) : items.length === 0 ? (
          <div className="empty-state">
            <Gauge size={27} />
            <h3>No active budgets</h3>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="pool-inventory-table budget-table">
              <thead>
                <tr>
                  <th>Budget</th>
                  <th>Scope</th>
                  <th>Window</th>
                  <th>Limit</th>
                  <th>Usage</th>
                  <th>Remaining</th>
                  <th>State</th>
                </tr>
              </thead>
              <tbody>
                {items.map((budget) => (
                  <tr key={budget.id}>
                    <td className="pool-name-cell" data-label="Budget">
                      <strong>{budget.name}</strong>
                      <span>{budget.id}</span>
                    </td>
                    <td data-label="Scope">{formatScope(budget)}</td>
                    <td data-label="Window">
                      <div className="budget-window">
                        <strong>{capitalize(budget.window)}</strong>
                        <span>{formatWindow(budget)}</span>
                      </div>
                    </td>
                    <td data-label="Limit">
                      {formatBytes(budget.limit_bytes)}
                    </td>
                    <td data-label="Usage">
                      <div className="budget-usage">
                        <progress
                          max={budget.limit_bytes}
                          value={Math.min(
                            budget.limit_bytes,
                            budget.used_bytes + budget.reserved_bytes,
                          )}
                          aria-label={`${budget.name} usage`}
                        />
                        <span>
                          {formatBytes(budget.used_bytes)} used
                          {budget.reserved_bytes > 0
                            ? ` · ${formatBytes(budget.reserved_bytes)} reserved`
                            : ""}
                        </span>
                      </div>
                    </td>
                    <td data-label="Remaining">
                      {formatBytes(budget.remaining_bytes)}
                    </td>
                    <td data-label="State">
                      <div className="budget-window">
                        <span
                          className={`client-state ${budget.exhausted ? "disabled" : "enabled"}`}
                        >
                          {budget.exhausted ? "Exhausted" : "Available"}
                        </span>
                        <span>
                          {budget.hard ? "Hard" : "Soft"} ·{" "}
                          {capitalize(budget.action)}
                        </span>
                      </div>
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

function capitalize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

export function formatScope(budget: BudgetStatus): string {
  return budget.scope_id
    ? `${budget.scope} · ${budget.scope_id}`
    : budget.scope;
}

export function formatWindow(budget: BudgetStatus): string {
  if (!budget.window_start || !budget.window_end) return "No reset";
  const start = new Date(budget.window_start);
  const end = new Date(budget.window_end);
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) return "—";
  const formatter = new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "short",
    ...(budget.timezone ? { timeZone: budget.timezone } : {}),
  });
  return `${formatter.format(start)} – ${formatter.format(end)}${budget.timezone ? ` · ${budget.timezone}` : ""}`;
}
