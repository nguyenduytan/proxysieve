import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  Edit3,
  Layers3,
  Plus,
  Power,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type {
  EndpointRecord,
  Pool,
  PoolPage,
  PoolRecord,
  PoolResponse,
  PoolStrategy,
  ProxyPage,
  Role,
} from "./api";

const strategies: readonly PoolStrategy[] = [
  "round-robin",
  "random",
  "weighted-random",
  "least-connections",
  "least-traffic",
  "lowest-latency",
  "highest-health",
  "lowest-cost",
  "cost-aware",
  "sticky",
];

export function PoolInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [records, setRecords] = useState<PoolRecord[]>([]);
  const [endpoints, setEndpoints] = useState<EndpointRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editor, setEditor] = useState<PoolRecord | "new" | null>(null);
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
        const [poolPage, proxyPage] = await Promise.all([
          fetchAllPools(signal),
          fetchAllEndpoints(signal),
        ]);
        if (signal?.aborted) return;
        setRecords(poolPage);
        setEndpoints(proxyPage);
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

  const endpointNames = useMemo(
    () =>
      new Map(
        endpoints.map(({ endpoint }) => [
          endpoint.id,
          endpoint.name || endpoint.host,
        ]),
      ),
    [endpoints],
  );
  const poolNames = useMemo(
    () => new Map(records.map(({ pool }) => [pool.id, pool.name])),
    [records],
  );

  async function toggle(record: PoolRecord) {
    setBusy(record.pool.id);
    setError("");
    setNotice("");
    try {
      const response = await api<PoolResponse>(
        `/api/v1/pools/${encodeURIComponent(record.pool.id)}`,
        {
          method: "PATCH",
          body: {
            pool: { ...record.pool, enabled: !record.pool.enabled },
            revision: record.revision,
          },
        },
      );
      setRecords((current) =>
        current.map((item) =>
          item.pool.id === response.pool.pool.id ? response.pool : item,
        ),
      );
      setNotice(
        `${record.pool.name} ${response.pool.pool.enabled ? "enabled" : "disabled"} and staged. Active routing is unchanged.`,
      );
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load(undefined, true);
        setError("This pool changed elsewhere. The pool list was refreshed.");
      } else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  async function remove(record: PoolRecord) {
    setBusy(record.pool.id);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/pools/${encodeURIComponent(record.pool.id)}`, {
        method: "DELETE",
        body: { revision: record.revision },
      });
      setRecords((current) =>
        current.filter((item) => item.pool.id !== record.pool.id),
      );
      setConfirming("");
      setNotice(`${record.pool.name} deleted from pool inventory.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load(undefined, true);
        setError(
          caught.code === "POOL_IN_USE"
            ? "This pool is still used as a fallback. Remove that reference first."
            : "This pool changed elsewhere. The pool list was refreshed.",
        );
      } else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Pools</h1>
          <p>
            Group proxy endpoints and define deterministic selection behavior.
          </p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh pools"
            aria-label="Refresh pools"
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
              Add pool
            </button>
          ) : null}
        </div>
      </header>
      <p className="scope-notice">
        Pool changes are staged as revisioned inventory. Activate the complete
        inventory from Policies when proxies, pools and policies are ready.
      </p>
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
        <PoolForm
          {...(editor === "new" ? {} : { initial: editor })}
          endpoints={endpoints}
          pools={records}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onConflict={(message) => {
            setEditor(null);
            void load(undefined, true).then(() => setError(message));
          }}
          onSaved={(record, created) => {
            setEditor(null);
            setRecords((current) =>
              created
                ? [...current, record].sort((a, b) =>
                    a.pool.id.localeCompare(b.pool.id),
                  )
                : current.map((item) =>
                    item.pool.id === record.pool.id ? record : item,
                  ),
            );
            setNotice(
              `${record.pool.name} ${created ? "created" : "updated"} and staged. Active routing is unchanged.`,
            );
          }}
        />
      ) : null}
      <section className="table-panel" aria-label="Routing pool inventory">
        <div className="section-header">
          <div>
            <h2>Pool inventory</h2>
            <span>
              {records.length} loaded ·{" "}
              {mutable ? "operator controls" : "read only"}
            </span>
          </div>
        </div>
        {loading && records.length === 0 ? (
          <div className="empty-state" role="status">
            Loading pools…
          </div>
        ) : records.length === 0 ? (
          <div className="empty-state">
            <Layers3 size={27} />
            <h3>No saved pools</h3>
            <p>
              {mutable
                ? "Create a pool after saving at least one proxy endpoint."
                : "An operator has not created any pool inventory yet."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="pool-inventory-table">
              <thead>
                <tr>
                  <th>Pool</th>
                  <th>Strategy</th>
                  <th>Endpoints</th>
                  <th>Fallbacks</th>
                  <th>Constraints</th>
                  <th>Status</th>
                  <th>Revision</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {records.map((record) => {
                  const pool = record.pool;
                  const working = busy === pool.id;
                  return (
                    <tr key={pool.id}>
                      <td className="pool-name-cell" data-label="Pool">
                        <strong>{pool.name}</strong>
                        <span>{pool.id}</span>
                      </td>
                      <td className="mono" data-label="Strategy">
                        {pool.strategy}
                      </td>
                      <td data-label="Endpoints">
                        {summarizeIDs(pool.endpoint_ids, endpointNames)}
                      </td>
                      <td data-label="Fallbacks">
                        {summarizeIDs(pool.fallback_pool_ids, poolNames)}
                      </td>
                      <td data-label="Constraints">
                        {summarizeConstraints(pool)}
                      </td>
                      <td data-label="Status">
                        <span
                          className={`client-state ${pool.enabled ? "enabled" : "disabled"}`}
                        >
                          {pool.enabled ? "Enabled" : "Disabled"}
                        </span>
                      </td>
                      <td data-label="Revision">{record.revision}</td>
                      {mutable ? (
                        <td className="pool-action-cell">
                          {confirming === pool.id ? (
                            <div
                              className="inline-confirm"
                              role="group"
                              aria-label={`Delete ${pool.name}`}
                            >
                              <button
                                className="danger-button"
                                disabled={working}
                                onClick={() => void remove(record)}
                              >
                                <Trash2 size={14} />
                                {working ? "Deleting…" : "Confirm delete"}
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`Cancel deleting ${pool.name}`}
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
                                title="Edit pool"
                                aria-label={`Edit ${pool.name}`}
                                disabled={working}
                                onClick={() => {
                                  setEditor(record);
                                  setConfirming("");
                                  setError("");
                                  setNotice("");
                                }}
                              >
                                <Edit3 size={14} />
                                <span>Edit</span>
                              </button>
                              <button
                                className="icon-button pool-action-button"
                                title={
                                  pool.enabled ? "Disable pool" : "Enable pool"
                                }
                                aria-label={`${pool.enabled ? "Disable" : "Enable"} ${pool.name}`}
                                disabled={working}
                                onClick={() => void toggle(record)}
                              >
                                <Power size={14} />
                                <span>
                                  {pool.enabled ? "Disable" : "Enable"}
                                </span>
                              </button>
                              <button
                                className="icon-button pool-action-button danger-icon"
                                title="Delete pool"
                                aria-label={`Delete ${pool.name}`}
                                disabled={working}
                                onClick={() => {
                                  setConfirming(pool.id);
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

function PoolForm({
  initial,
  endpoints,
  pools,
  onSaved,
  onCancel,
  onExpired,
  onConflict,
}: {
  initial?: PoolRecord;
  endpoints: EndpointRecord[];
  pools: PoolRecord[];
  onSaved: (record: PoolRecord, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
  onConflict: (message: string) => void;
}) {
  const source = initial?.pool;
  const [name, setName] = useState(source?.name ?? "");
  const [strategy, setStrategy] = useState<PoolStrategy>(
    source?.strategy ?? "round-robin",
  );
  const [endpointIDs, setEndpointIDs] = useState<string[]>(
    source?.endpoint_ids ?? [],
  );
  const [fallbackIDs, setFallbackIDs] = useState<string[]>(
    source?.fallback_pool_ids ?? [],
  );
  const [tags, setTags] = useState((source?.required_tags ?? []).join(", "));
  const [country, setCountry] = useState(source?.country ?? "");
  const [minHealth, setMinHealth] = useState(
    String(source?.min_health_score ?? 0),
  );
  const [maxLatency, setMaxLatency] = useState(
    String((source?.max_latency_ns ?? 0) / 1_000_000),
  );
  const [enabled, setEnabled] = useState(source?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const health = Number(minHealth);
    const latency = Number(maxLatency);
    if (!Number.isInteger(health) || health < 0 || health > 100) {
      setError("Minimum health score must be an integer from 0 to 100.");
      return;
    }
    if (!Number.isFinite(latency) || latency < 0 || latency > 86_400_000) {
      setError(
        "Maximum latency must be between 0 and 86,400,000 milliseconds.",
      );
      return;
    }
    const requiredTags = tags
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean);
    if (new Set(requiredTags).size !== requiredTags.length) {
      setError("Required tags must be unique.");
      return;
    }
    setBusy(true);
    setError("");
    const pool: Pool = {
      id: source?.id ?? "",
      name: name.trim(),
      strategy,
      endpoint_ids: endpointIDs,
      fallback_pool_ids: fallbackIDs,
      required_tags: requiredTags,
      country: country.trim(),
      min_health_score: health,
      max_latency_ns: Math.round(latency * 1_000_000),
      enabled,
    };
    try {
      const response = await api<PoolResponse>(
        initial
          ? `/api/v1/pools/${encodeURIComponent(initial.pool.id)}`
          : "/api/v1/pools",
        {
          method: initial ? "PATCH" : "POST",
          body: initial ? { pool, revision: initial.revision } : { pool },
        },
      );
      onSaved(response.pool, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409)
        onConflict("This pool changed elsewhere. The pool list was refreshed.");
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form pool-form" onSubmit={save}>
      <h2>{initial ? "Edit pool" : "Create pool"}</h2>
      <div className="form-grid">
        <label>
          Name
          <input
            required
            maxLength={256}
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          Selection strategy
          <select
            value={strategy}
            onChange={(event) =>
              setStrategy(event.target.value as PoolStrategy)
            }
          >
            {strategies.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <label>
          Country constraint (optional)
          <input
            maxLength={128}
            placeholder="US"
            value={country}
            onChange={(event) => setCountry(event.target.value)}
          />
        </label>
        <label>
          Required tags (comma-separated)
          <input
            placeholder="residential, premium"
            value={tags}
            onChange={(event) => setTags(event.target.value)}
          />
        </label>
        <label>
          Minimum health score
          <input
            type="number"
            min={0}
            max={100}
            step={1}
            value={minHealth}
            onChange={(event) => setMinHealth(event.target.value)}
          />
        </label>
        <label>
          Maximum latency (ms, 0 disables)
          <input
            type="number"
            min={0}
            max={86_400_000}
            step={1}
            value={maxLatency}
            onChange={(event) => setMaxLatency(event.target.value)}
          />
        </label>
        <ChoiceGroup
          legend="Proxy endpoints"
          empty="Save a proxy endpoint before assigning pool members."
          choices={endpoints.map(({ endpoint }) => ({
            id: endpoint.id,
            label: endpoint.name || endpoint.host,
            detail: `${endpoint.protocol}://${endpoint.host}:${endpoint.port}`,
          }))}
          selected={endpointIDs}
          onChange={setEndpointIDs}
        />
        <ChoiceGroup
          legend="Fallback pools"
          empty="No other pools are available."
          choices={pools
            .filter(({ pool }) => pool.id !== source?.id)
            .map(({ pool }) => ({
              id: pool.id,
              label: pool.name,
              detail: pool.strategy,
            }))}
          selected={fallbackIDs}
          onChange={setFallbackIDs}
        />
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Enable this pool in inventory
        </label>
      </div>
      <p className="field-help">
        Required tags, country, health and latency constraints are applied by
        the runtime selector after the complete inventory is activated.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create pool"}
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

function ChoiceGroup({
  legend,
  empty,
  choices,
  selected,
  onChange,
}: {
  legend: string;
  empty: string;
  choices: { id: string; label: string; detail: string }[];
  selected: string[];
  onChange: (ids: string[]) => void;
}) {
  return (
    <fieldset className="choice-group form-span">
      <legend>{legend}</legend>
      {choices.length === 0 ? (
        <p>{empty}</p>
      ) : (
        <div className="choice-grid">
          {choices.map((choice) => (
            <label key={choice.id}>
              <input
                type="checkbox"
                checked={selected.includes(choice.id)}
                onChange={(event) =>
                  onChange(
                    event.target.checked
                      ? [...selected, choice.id]
                      : selected.filter((id) => id !== choice.id),
                  )
                }
              />
              <span>
                <strong>{choice.label}</strong>
                <small>{choice.detail}</small>
              </span>
            </label>
          ))}
        </div>
      )}
    </fieldset>
  );
}

function summarizeIDs(ids: string[], names: Map<string, string>): string {
  const firstID = ids[0];
  if (!firstID) return "—";
  const first = names.get(firstID) ?? firstID;
  return ids.length === 1 ? first : `${first} +${ids.length - 1}`;
}

function summarizeConstraints(pool: Pool): string {
  const values = [];
  if (pool.country) values.push(pool.country);
  if (pool.required_tags.length > 0) values.push(pool.required_tags.join(", "));
  if (pool.min_health_score > 0)
    values.push(`health ≥ ${pool.min_health_score}`);
  if (pool.max_latency_ns > 0)
    values.push(`latency ≤ ${pool.max_latency_ns / 1_000_000} ms`);
  return values.join(" · ") || "None";
}

async function fetchAllPools(signal?: AbortSignal): Promise<PoolRecord[]> {
  const records: PoolRecord[] = [];
  let after = "";
  do {
    const query = new URLSearchParams({ limit: "1000" });
    if (after) query.set("after", after);
    const page = await api<PoolPage>(
      `/api/v1/pools?${query}`,
      signal ? { signal } : {},
    );
    records.push(...page.items);
    if (page.next_after && page.next_after === after)
      throw new ApiError(0, "The pool inventory returned an invalid page.");
    after = page.next_after;
  } while (after);
  return records;
}

async function fetchAllEndpoints(
  signal?: AbortSignal,
): Promise<EndpointRecord[]> {
  const records: EndpointRecord[] = [];
  let after = "";
  do {
    const query = new URLSearchParams({ limit: "1000" });
    if (after) query.set("after", after);
    const page = await api<ProxyPage>(
      `/api/v1/proxies?${query}`,
      signal ? { signal } : {},
    );
    records.push(...page.items);
    if (page.next_after && page.next_after === after)
      throw new ApiError(0, "The proxy inventory returned an invalid page.");
    after = page.next_after;
  } while (after);
  return records;
}
