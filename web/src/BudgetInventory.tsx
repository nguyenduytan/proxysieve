import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Edit3, Gauge, Plus, RefreshCw, Trash2, X } from "lucide-react";
import {
  ApiError,
  api,
  errorMessage,
  formatBytes,
  type BudgetConfig,
  type BudgetScope,
  type BudgetStatus,
  type BudgetStatusPage,
  type BudgetWindow,
  type Role,
} from "./api";

export function BudgetInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [items, setItems] = useState<BudgetStatus[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editor, setEditor] = useState<BudgetStatus | "new" | null>(null);
  const [confirming, setConfirming] = useState("");
  const [busy, setBusy] = useState("");
  const mutable = role !== "viewer";

  const load = useCallback(
    async (signal?: AbortSignal, preserveFeedback = false) => {
      setLoading(true);
      if (!preserveFeedback) {
        setError("");
        setNotice("");
      }
      try {
        const page = await api<BudgetStatusPage>(
          "/api/v1/budgets",
          signal ? { signal } : {},
        );
        if (!signal?.aborted) setItems(page.items);
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

  async function remove(item: BudgetStatus) {
    setBusy(item.id);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/budgets/${encodeURIComponent(item.id)}`, {
        method: "DELETE",
        body: { revision: item.revision },
      });
      setItems((current) => current.filter((budget) => budget.id !== item.id));
      setConfirming("");
      setNotice(`${item.name} deleted.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load(undefined, true);
        setError("This budget changed elsewhere. The list was refreshed.");
      } else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Budgets</h1>
          <p>Control paid upstream traffic with durable byte limits.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh budgets"
            aria-label="Refresh budgets"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
          {mutable ? (
            <button
              className="command-button"
              onClick={() => {
                setEditor(editor === "new" ? null : "new");
                setConfirming("");
                setError("");
                setNotice("");
              }}
            >
              <Plus size={16} />
              Add budget
            </button>
          ) : null}
        </div>
      </header>
      {!editor && error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      {!editor && notice ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      {editor ? (
        <BudgetForm
          {...(editor === "new" ? {} : { initial: editor })}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onConflict={(message) => {
            setEditor(null);
            void load(undefined, true).then(() => setError(message));
          }}
          onSaved={(saved, created) => {
            setEditor(null);
            setItems((current) =>
              created
                ? [...current, saved].sort((a, b) => a.id.localeCompare(b.id))
                : current.map((item) => (item.id === saved.id ? saved : item)),
            );
            setNotice(`${saved.name} ${created ? "created" : "updated"}.`);
          }}
        />
      ) : null}
      <section className="table-panel" aria-label="Budget inventory">
        <div className="section-header">
          <div>
            <h2>Active limits</h2>
            <span>
              {items.length} loaded ·{" "}
              {mutable ? "operator controls" : "read only"}
            </span>
          </div>
        </div>
        {loading && items.length === 0 ? (
          <div className="empty-state" role="status">
            Loading budget usage…
          </div>
        ) : items.length === 0 ? (
          <div className="empty-state">
            <Gauge size={27} />
            <h3>No active budgets</h3>
            <p>
              {mutable
                ? "Create a hard limit for system, client, pool or proxy traffic."
                : "An operator has not created any budgets yet."}
            </p>
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
                  <th>Revision</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {items.map((budget) => {
                  const working = busy === budget.id;
                  return (
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
                            {budget.hard ? "Hard reject" : "Tracking only"}
                          </span>
                        </div>
                      </td>
                      <td data-label="Revision">{budget.revision}</td>
                      {mutable ? (
                        <td className="pool-action-cell">
                          {confirming === budget.id ? (
                            <div
                              className="inline-confirm"
                              role="group"
                              aria-label={`Delete ${budget.name}`}
                            >
                              <button
                                className="danger-button"
                                disabled={working}
                                onClick={() => void remove(budget)}
                              >
                                <Trash2 size={14} />
                                {working ? "Deleting…" : "Confirm delete"}
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`Cancel deleting ${budget.name}`}
                                disabled={working}
                                onClick={() => setConfirming("")}
                              >
                                <X size={14} />
                              </button>
                            </div>
                          ) : (
                            <div className="row-actions">
                              <button
                                className="icon-button pool-action-button"
                                title="Edit budget"
                                aria-label={`Edit ${budget.name}`}
                                disabled={working}
                                onClick={() => {
                                  setEditor(budget);
                                  setConfirming("");
                                  setError("");
                                  setNotice("");
                                }}
                              >
                                <Edit3 size={14} />
                                <span>Edit</span>
                              </button>
                              <button
                                className="icon-button pool-action-button danger-icon"
                                title="Delete budget"
                                aria-label={`Delete ${budget.name}`}
                                disabled={working}
                                onClick={() => {
                                  setConfirming(budget.id);
                                  setEditor(null);
                                  setError("");
                                  setNotice("");
                                }}
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

function BudgetForm({
  initial,
  onSaved,
  onCancel,
  onExpired,
  onConflict,
}: {
  initial?: BudgetStatus;
  onSaved: (budget: BudgetStatus, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
  onConflict: (message: string) => void;
}) {
  const [id, setID] = useState(initial?.id ?? "");
  const [name, setName] = useState(initial?.name ?? "");
  const [scope, setScope] = useState<BudgetScope>(initial?.scope ?? "system");
  const [scopeID, setScopeID] = useState(initial?.scope_id ?? "");
  const [limit, setLimit] = useState(String(initial?.limit_bytes ?? ""));
  const [window, setWindow] = useState<BudgetWindow>(
    initial?.window ?? "lifetime",
  );
  const [timezone, setTimezone] = useState(initial?.timezone ?? "UTC");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const limitBytes = Number(limit);
    if (!Number.isSafeInteger(limitBytes) || limitBytes < 1) {
      setError(
        "Limit must be a positive integer within the browser safe range.",
      );
      return;
    }
    if (scope !== "system" && !scopeID.trim()) {
      setError("Scope ID is required for client, pool and proxy budgets.");
      return;
    }
    if (window !== "lifetime" && !timezone.trim()) {
      setError("Timezone is required for calendar budgets.");
      return;
    }
    const budget: BudgetConfig = {
      id: id.trim(),
      name: name.trim(),
      scope,
      ...(scope === "system" ? {} : { scope_id: scopeID.trim() }),
      limit_bytes: limitBytes,
      hard: initial?.hard ?? true,
      action: initial?.action ?? "reject",
      window,
      ...(window === "lifetime" ? {} : { timezone: timezone.trim() }),
    };
    setBusy(true);
    setError("");
    try {
      const response = await api<BudgetStatus>(
        initial
          ? `/api/v1/budgets/${encodeURIComponent(initial.id)}`
          : "/api/v1/budgets",
        {
          method: initial ? "PATCH" : "POST",
          body: initial ? { budget, revision: initial.revision } : { budget },
        },
      );
      onSaved(response, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409)
        onConflict("This budget changed elsewhere. The list was refreshed.");
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form budget-form" onSubmit={save}>
      <h2>{initial ? "Edit budget" : "Create budget"}</h2>
      <div className="form-grid">
        <label>
          ID
          <input
            required
            maxLength={128}
            autoFocus={!initial}
            readOnly={Boolean(initial)}
            value={id}
            onChange={(event) => setID(event.target.value)}
          />
        </label>
        <label>
          Name
          <input
            required
            maxLength={256}
            autoFocus={Boolean(initial)}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          Scope
          <select
            value={scope}
            onChange={(event) => setScope(event.target.value as BudgetScope)}
          >
            <option value="system">System</option>
            <option value="client">Client</option>
            <option value="pool">Pool</option>
            <option value="proxy">Proxy</option>
          </select>
        </label>
        {scope !== "system" ? (
          <label>
            Scope ID
            <input
              required
              maxLength={128}
              value={scopeID}
              onChange={(event) => setScopeID(event.target.value)}
            />
          </label>
        ) : null}
        <label>
          Limit (bytes)
          <input
            required
            type="number"
            min={1}
            max={Number.MAX_SAFE_INTEGER}
            step={1}
            value={limit}
            onChange={(event) => setLimit(event.target.value)}
          />
        </label>
        <label>
          Window
          <select
            value={window}
            onChange={(event) => setWindow(event.target.value as BudgetWindow)}
          >
            <option value="lifetime">Lifetime</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
            <option value="monthly">Monthly</option>
          </select>
        </label>
        {window !== "lifetime" ? (
          <label>
            IANA timezone
            <input
              required
              maxLength={128}
              placeholder="UTC"
              value={timezone}
              onChange={(event) => setTimezone(event.target.value)}
            />
          </label>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create budget"}
        </button>
        <button
          type="button"
          className="pause-button secondary"
          disabled={busy}
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
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
