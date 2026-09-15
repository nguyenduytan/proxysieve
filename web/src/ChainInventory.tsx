import { useCallback, useEffect, useMemo, useState } from "react";
import type { FormEvent } from "react";
import {
  ArrowDown,
  ArrowUp,
  Edit3,
  Link2,
  Plus,
  Power,
  RefreshCw,
  TestTube2,
  Trash2,
  X,
} from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type {
  ChainHop,
  ChainHealth,
  ChainPage,
  ChainResponse,
  ChainTestResponse,
  PoolPage,
  PoolRecord,
  ProxyChain,
  Role,
} from "./api";

const defaultHop = (): ChainHop => ({
  pool_id: "",
  timeout_ns: 15_000_000_000,
});

export function ChainInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [records, setRecords] = useState<ChainResponse[]>([]);
  const [pools, setPools] = useState<PoolRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editor, setEditor] = useState<ChainResponse | "new" | null>(null);
  const [tester, setTester] = useState<ChainResponse | null>(null);
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
        const [chains, poolRecords] = await Promise.all([
          fetchAllChains(signal),
          fetchAllPools(signal),
        ]);
        if (signal?.aborted) return;
        setRecords(chains);
        setPools(poolRecords);
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

  const poolNames = useMemo(
    () => new Map(pools.map(({ pool }) => [pool.id, pool.name])),
    [pools],
  );

  async function toggle(record: ChainResponse) {
    setBusy(record.chain.chain.id);
    setError("");
    setNotice("");
    try {
      const response = await api<ChainResponse>(
        `/api/v1/chains/${encodeURIComponent(record.chain.chain.id)}`,
        {
          method: "PATCH",
          body: {
            chain: {
              ...record.chain.chain,
              enabled: !record.chain.chain.enabled,
            },
            revision: record.chain.revision,
          },
        },
      );
      setRecords((current) =>
        current.map((item) =>
          item.chain.chain.id === response.chain.chain.id ? response : item,
        ),
      );
      setNotice(
        `${record.chain.chain.name} ${response.chain.chain.enabled ? "enabled" : "disabled"} and staged. Active routing is unchanged.`,
      );
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load(undefined, true);
        setError("This chain changed elsewhere. The chain list was refreshed.");
      } else setError(errorMessage(caught));
    } finally {
      setBusy("");
    }
  }

  async function remove(record: ChainResponse) {
    const chain = record.chain.chain;
    setBusy(chain.id);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/chains/${encodeURIComponent(chain.id)}`, {
        method: "DELETE",
        body: { revision: record.chain.revision },
      });
      setRecords((current) =>
        current.filter((item) => item.chain.chain.id !== chain.id),
      );
      setConfirming("");
      setNotice(`${chain.name} deleted from chain inventory.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load(undefined, true);
        setError(
          caught.code === "CHAIN_IN_USE"
            ? "This chain is referenced by a saved policy. Remove that reference first."
            : "This chain changed elsewhere. The chain list was refreshed.",
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
          <h1>Chains</h1>
          <p>
            Route traffic through ordered proxy pools with bounded hop timeouts.
          </p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh chains"
            aria-label="Refresh chains"
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
                setTester(null);
                setError("");
                setNotice("");
              }}
            >
              <Plus size={16} />
              Add chain
            </button>
          ) : null}
        </div>
      </header>
      <p className="scope-notice">
        Chain changes are staged with the rest of routing inventory. Activate
        the complete inventory from Policies when chains and their policies are
        ready.
      </p>
      {!editor && !tester && error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      {!editor && !tester && notice ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      {editor ? (
        <ChainForm
          {...(editor === "new" ? {} : { initial: editor })}
          pools={pools}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onConflict={(message) => {
            setEditor(null);
            void load(undefined, true).then(() => setError(message));
          }}
          onSaved={(response, created) => {
            setEditor(null);
            setRecords((current) =>
              created
                ? [...current, response].sort((a, b) =>
                    a.chain.chain.id.localeCompare(b.chain.chain.id),
                  )
                : current.map((item) =>
                    item.chain.chain.id === response.chain.chain.id
                      ? response
                      : item,
                  ),
            );
            setNotice(
              `${response.chain.chain.name} ${created ? "created" : "updated"} and staged. Active routing is unchanged.`,
            );
          }}
        />
      ) : null}
      {tester ? (
        <ChainTestForm
          record={tester}
          onCancel={() => setTester(null)}
          onExpired={onExpired}
          onTested={(health) => {
            setTester(null);
            setRecords((current) =>
              current.map((item) =>
                item.chain.chain.id === tester.chain.chain.id
                  ? { ...item, health }
                  : item,
              ),
            );
            if (health.status === "healthy") {
              setNotice(
                `${tester.chain.chain.name} reached the target in ${formatLatency(health.latency_ns)}.`,
              );
            } else {
              setError(
                `${tester.chain.chain.name} test failed${health.failed_hop ? ` at hop ${health.failed_hop}` : ""}.`,
              );
            }
          }}
        />
      ) : null}
      <section className="table-panel" aria-label="Proxy chain inventory">
        <div className="section-header">
          <div>
            <h2>Chain inventory</h2>
            <span>
              {records.length} loaded ·{" "}
              {mutable ? "operator controls" : "read only"}
            </span>
          </div>
        </div>
        {loading && records.length === 0 ? (
          <div className="empty-state" role="status">
            Loading chains…
          </div>
        ) : records.length === 0 ? (
          <div className="empty-state">
            <Link2 size={27} />
            <h3>No saved chains</h3>
            <p>
              {mutable
                ? "Create a chain after saving at least two independent pools."
                : "An operator has not created any chain inventory yet."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="pool-inventory-table">
              <thead>
                <tr>
                  <th>Chain</th>
                  <th>Ordered hops</th>
                  <th>Status</th>
                  <th>Runtime</th>
                  <th>Health</th>
                  <th>Revision</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {records.map((record) => {
                  const chain = record.chain.chain;
                  const working = busy === chain.id;
                  return (
                    <tr key={chain.id}>
                      <td className="pool-name-cell" data-label="Chain">
                        <strong>{chain.name}</strong>
                        <span>{chain.id}</span>
                      </td>
                      <td data-label="Ordered hops">
                        {chain.hops
                          .map(
                            (hop, index) =>
                              `${index + 1}. ${poolNames.get(hop.pool_id) ?? hop.pool_id} · ${hop.timeout_ns / 1_000_000} ms`,
                          )
                          .join(" → ")}
                      </td>
                      <td data-label="Status">
                        <span
                          className={`client-state ${chain.enabled ? "enabled" : "disabled"}`}
                        >
                          {chain.enabled ? "Enabled" : "Disabled"}
                        </span>
                      </td>
                      <td data-label="Runtime">
                        <span
                          className={`client-state ${record.runtime_active ? "enabled" : "disabled"}`}
                        >
                          {record.activation}
                        </span>
                      </td>
                      <td data-label="Health">{formatHealth(record)}</td>
                      <td data-label="Revision">{record.chain.revision}</td>
                      {mutable ? (
                        <td className="pool-action-cell">
                          {confirming === chain.id ? (
                            <div
                              className="inline-confirm"
                              role="group"
                              aria-label={`Delete ${chain.name}`}
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
                                aria-label={`Cancel deleting ${chain.name}`}
                                disabled={working}
                                onClick={() => setConfirming("")}
                              >
                                <X size={14} />
                              </button>
                            </div>
                          ) : (
                            <div className="row-actions chain-action-row">
                              <button
                                className="icon-button pool-action-button"
                                title="Edit chain"
                                aria-label={`Edit ${chain.name}`}
                                disabled={working}
                                onClick={() => {
                                  setEditor(record);
                                  setConfirming("");
                                  setTester(null);
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
                                  record.runtime_active && chain.enabled
                                    ? "Test active chain"
                                    : "Enable and activate this revision before testing"
                                }
                                aria-label={`Test ${chain.name}`}
                                disabled={
                                  working ||
                                  !record.runtime_active ||
                                  !chain.enabled
                                }
                                onClick={() => {
                                  setTester(record);
                                  setEditor(null);
                                  setConfirming("");
                                  setError("");
                                  setNotice("");
                                }}
                              >
                                <TestTube2 size={14} />
                                <span>Test</span>
                              </button>
                              <button
                                className="icon-button pool-action-button"
                                title={
                                  chain.enabled
                                    ? "Disable chain"
                                    : "Enable chain"
                                }
                                aria-label={`${chain.enabled ? "Disable" : "Enable"} ${chain.name}`}
                                disabled={working}
                                onClick={() => void toggle(record)}
                              >
                                <Power size={14} />
                                <span>
                                  {chain.enabled ? "Disable" : "Enable"}
                                </span>
                              </button>
                              <button
                                className="icon-button pool-action-button danger-icon"
                                title="Delete chain"
                                aria-label={`Delete ${chain.name}`}
                                disabled={working}
                                onClick={() => {
                                  setConfirming(chain.id);
                                  setEditor(null);
                                  setTester(null);
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

function ChainTestForm({
  record,
  onTested,
  onCancel,
  onExpired,
}: {
  record: ChainResponse;
  onTested: (health: ChainHealth) => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [host, setHost] = useState("");
  const [port, setPort] = useState("443");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function test(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const targetPort = Number(port);
    if (!Number.isInteger(targetPort) || targetPort < 1 || targetPort > 65535) {
      setError("Target port must be an integer from 1 to 65,535.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const response = await api<ChainTestResponse>(
        `/api/v1/chains/${encodeURIComponent(record.chain.chain.id)}/test`,
        {
          method: "POST",
          timeoutMs: 35_000,
          body: { target_host: host.trim(), target_port: targetPort },
        },
      );
      onTested(response.result);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form" onSubmit={test}>
      <h2>Test {record.chain.chain.name}</h2>
      <div className="form-grid">
        <label>
          Target host
          <input
            required
            maxLength={253}
            autoFocus
            placeholder="example.com"
            value={host}
            onChange={(event) => setHost(event.target.value)}
          />
        </label>
        <label>
          Target port
          <input
            required
            type="number"
            min={1}
            max={65535}
            step={1}
            value={port}
            onChange={(event) => setPort(event.target.value)}
          />
        </label>
      </div>
      <p className="field-help">
        This opens and immediately closes one TCP connection through every
        active hop. No application payload is sent.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          <TestTube2 size={15} />
          {busy ? "Testing…" : "Run test"}
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

function ChainForm({
  initial,
  pools,
  onSaved,
  onCancel,
  onExpired,
  onConflict,
}: {
  initial?: ChainResponse;
  pools: PoolRecord[];
  onSaved: (response: ChainResponse, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
  onConflict: (message: string) => void;
}) {
  const source = initial?.chain.chain;
  const [name, setName] = useState(source?.name ?? "");
  const [hops, setHops] = useState<ChainHop[]>(
    source?.hops.map((hop) => ({ ...hop })) ?? [defaultHop(), defaultHop()],
  );
  const [enabled, setEnabled] = useState(source?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function updateHop(index: number, patch: Partial<ChainHop>) {
    setHops((current) =>
      current.map((hop, hopIndex) =>
        hopIndex === index ? { ...hop, ...patch } : hop,
      ),
    );
  }

  function moveHop(index: number, offset: -1 | 1) {
    setHops((current) => {
      const target = index + offset;
      if (target < 0 || target >= current.length) return current;
      const sourceHop = current[index];
      const targetHop = current[target];
      if (!sourceHop || !targetHop) return current;
      const next = [...current];
      next[index] = targetHop;
      next[target] = sourceHop;
      return next;
    });
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (hops.length < 2 || hops.length > 8) {
      setError("A chain must contain 2 to 8 hops.");
      return;
    }
    if (hops.some((hop) => !hop.pool_id)) {
      setError("Choose a pool for every hop.");
      return;
    }
    if (new Set(hops.map((hop) => hop.pool_id)).size !== hops.length) {
      setError("Each pool can appear only once in a chain.");
      return;
    }
    if (
      hops.some(
        (hop) =>
          !Number.isSafeInteger(hop.timeout_ns) ||
          hop.timeout_ns < 0 ||
          hop.timeout_ns > 120_000_000_000,
      )
    ) {
      setError("Hop timeouts must be whole milliseconds from 0 to 120,000.");
      return;
    }
    const chain: ProxyChain = {
      id: source?.id ?? "",
      name: name.trim(),
      hops,
      enabled,
    };
    setBusy(true);
    setError("");
    try {
      const response = await api<ChainResponse>(
        initial
          ? `/api/v1/chains/${encodeURIComponent(initial.chain.chain.id)}`
          : "/api/v1/chains",
        {
          method: initial ? "PATCH" : "POST",
          body: initial
            ? { chain, revision: initial.chain.revision }
            : { chain },
        },
      );
      onSaved(response, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409)
        onConflict(
          "This chain changed elsewhere. The chain list was refreshed.",
        );
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit chain" : "Create chain"}</h2>
      <div className="form-grid">
        <label className="form-span">
          Name
          <input
            required
            maxLength={256}
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <fieldset className="chain-hops form-span">
          <legend>Ordered hops</legend>
          {hops.map((hop, index) => (
            <div className="chain-hop" key={index}>
              <span className="chain-hop-number">{index + 1}</span>
              <label>
                Pool
                <select
                  required
                  value={hop.pool_id}
                  onChange={(event) =>
                    updateHop(index, { pool_id: event.target.value })
                  }
                >
                  <option value="">Choose pool</option>
                  {pools.map(({ pool }) => (
                    <option
                      key={pool.id}
                      value={pool.id}
                      disabled={hops.some(
                        (selected, selectedIndex) =>
                          selectedIndex !== index &&
                          selected.pool_id === pool.id,
                      )}
                    >
                      {pool.name} ({pool.id})
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Timeout (ms)
                <input
                  type="number"
                  min={0}
                  max={120000}
                  step={1}
                  value={hop.timeout_ns / 1_000_000}
                  onChange={(event) =>
                    updateHop(index, {
                      timeout_ns: Math.round(
                        Number(event.target.value) * 1_000_000,
                      ),
                    })
                  }
                />
              </label>
              <div className="chain-hop-actions">
                <button
                  type="button"
                  className="icon-button"
                  title="Move hop up"
                  aria-label={`Move hop ${index + 1} up`}
                  disabled={index === 0}
                  onClick={() => moveHop(index, -1)}
                >
                  <ArrowUp size={15} />
                </button>
                <button
                  type="button"
                  className="icon-button"
                  title="Move hop down"
                  aria-label={`Move hop ${index + 1} down`}
                  disabled={index === hops.length - 1}
                  onClick={() => moveHop(index, 1)}
                >
                  <ArrowDown size={15} />
                </button>
                <button
                  type="button"
                  className="icon-button danger-icon"
                  title="Remove hop"
                  aria-label={`Remove hop ${index + 1}`}
                  disabled={hops.length <= 2}
                  onClick={() =>
                    setHops((current) => current.filter((_, i) => i !== index))
                  }
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </div>
          ))}
          {pools.length === 0 ? (
            <p className="field-help">
              Save at least two independent pools first.
            </p>
          ) : null}
          <button
            type="button"
            className="pause-button secondary chain-add-hop"
            disabled={hops.length >= 8 || hops.length >= pools.length}
            onClick={() => setHops((current) => [...current, defaultHop()])}
          >
            <Plus size={15} />
            Add hop
          </button>
        </fieldset>
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Enable this chain in inventory
        </label>
      </div>
      <p className="field-help">
        Every hop is mandatory and runs in this order. A zero timeout uses the
        15-second default. Hop pools must not share endpoints or sticky
        sessions.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy || pools.length < 2}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create chain"}
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

async function fetchAllChains(signal?: AbortSignal): Promise<ChainResponse[]> {
  const records: ChainResponse[] = [];
  let after = "";
  do {
    const query = new URLSearchParams({ limit: "1000" });
    if (after) query.set("after", after);
    const page = await api<ChainPage>(
      `/api/v1/chains?${query}`,
      signal ? { signal } : {},
    );
    records.push(...page.items);
    if (page.next_after && page.next_after === after)
      throw new ApiError(0, "The chain inventory returned an invalid page.");
    after = page.next_after;
  } while (after);
  return records;
}

function formatLatency(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return "—";
  return `${(value / 1_000_000).toFixed(value < 10_000_000 ? 1 : 0)} ms`;
}

function formatHealth(record: ChainResponse): string {
  if (!record.runtime_active) return "Not active";
  const health = record.health;
  if (!health || health.status === "untested") return "Untested";
  if (health.status === "healthy")
    return `Healthy · ${formatLatency(health.latency_ns)}`;
  return health.failed_hop
    ? `Failed at hop ${health.failed_hop}`
    : "Unavailable";
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
