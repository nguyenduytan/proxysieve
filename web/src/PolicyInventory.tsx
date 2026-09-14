import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
  Edit3,
  FileJson,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Trash2,
  X,
} from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type {
  Policy,
  PolicyPage,
  PolicyRecord,
  PolicyResponse,
  PolicySimulation,
  Role,
  RuntimeHistory,
  RuntimeState,
} from "./api";

const examplePolicy = (): Policy => ({
  version: 1,
  id: "default",
  name: "Proxy policy",
  rules: [
    {
      id: "route",
      name: "Route example hosts",
      priority: 100,
      enabled: true,
      stop_processing: true,
      conditions: {
        field: "host",
        operator: "suffix",
        values: ["example.invalid"],
      },
      actions: [{ type: "reject" }],
    },
  ],
});

export function PolicyInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [records, setRecords] = useState<PolicyRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editor, setEditor] = useState<PolicyRecord | "new" | null>(null);
  const [confirming, setConfirming] = useState("");
  const [simulation, setSimulation] = useState<PolicySimulation | null>(null);
  const [runtime, setRuntime] = useState<RuntimeState | null>(null);
  const [history, setHistory] = useState<RuntimeState[]>([]);
  const [rollbackRevision, setRollbackRevision] = useState("");
  const [confirmingActivation, setConfirmingActivation] = useState(false);
  const [confirmingRollback, setConfirmingRollback] = useState(false);
  const [runtimeBusy, setRuntimeBusy] = useState(false);
  const mutable = role !== "viewer";
  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const [page, runtimeState, runtimeHistory] = await Promise.all([
          api<PolicyPage>(
            "/api/v1/policies?limit=1000",
            signal ? { signal } : {},
          ),
          api<RuntimeState>("/api/v1/runtime", signal ? { signal } : {}),
          api<RuntimeHistory>(
            "/api/v1/runtime/history?limit=20",
            signal ? { signal } : {},
          ),
        ]);
        if (!signal?.aborted) {
          setRecords(page.items);
          setRuntime(runtimeState);
          setHistory(runtimeHistory.items ?? []);
          setConfirmingRollback(false);
          setRollbackRevision((current) =>
            runtimeHistory.items?.some(
              (item) => String(item.revision) === current,
            )
              ? current
              : "",
          );
        }
      } catch (caught) {
        if (!signal?.aborted) {
          if (caught instanceof ApiError && caught.status === 401) onExpired();
          else setError(errorMessage(caught));
        }
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
  async function remove(record: PolicyRecord) {
    try {
      await api<void>(
        `/api/v1/policies/${encodeURIComponent(record.policy.id)}`,
        { method: "DELETE", body: { revision: record.revision } },
      );
      setRecords((items) =>
        items.filter((item) => item.policy.id !== record.policy.id),
      );
      setRuntime((current) =>
        current ? { ...current, staged_changes: true } : current,
      );
      setSimulation(null);
      setConfirming("");
      setNotice(`${record.policy.name} deleted from policy inventory.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    }
  }
  async function simulate(record: PolicyRecord) {
    try {
      const result = await api<PolicySimulation>(
        `/api/v1/policies/${encodeURIComponent(record.policy.id)}/simulate`,
        {
          method: "POST",
          body: {
            listener: "http",
            protocol: "http",
            host: "api.example.invalid",
            port: 443,
            method: "GET",
            path: "/",
          },
        },
      );
      setSimulation(result);
      setError("");
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    }
  }
  async function activate() {
    if (!runtime) return;
    setRuntimeBusy(true);
    setError("");
    setNotice("");
    try {
      const activated = await api<RuntimeState>("/api/v1/runtime/activate", {
        method: "POST",
        body: { expected_revision: runtime.revision },
      });
      setRuntime(activated);
      setConfirmingActivation(false);
      setNotice(
        `Saved inventory activated as runtime revision ${activated.revision}.`,
      );
      await load();
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setRuntimeBusy(false);
    }
  }
  async function rollback() {
    if (!runtime || !rollbackRevision) return;
    setRuntimeBusy(true);
    setError("");
    setNotice("");
    try {
      const rolledBack = await api<RuntimeState>("/api/v1/runtime/rollback", {
        method: "POST",
        body: {
          expected_revision: runtime.revision,
          target_revision: Number(rollbackRevision),
        },
      });
      setRuntime(rolledBack);
      setRollbackRevision("");
      setConfirmingRollback(false);
      setNotice(
        `Runtime revision ${rolledBack.revision} restored from revision ${rolledBack.source_revision}.`,
      );
      await load();
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setRuntimeBusy(false);
    }
  }
  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Policies</h1>
          <p>Author and simulate deterministic routing rules.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh policies"
            aria-label="Refresh policies"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
          {mutable ? (
            <button
              className="command-button"
              disabled={
                loading || runtimeBusy || !runtime || !runtime.staged_changes
              }
              title={
                runtime?.staged_changes
                  ? "Activate staged inventory"
                  : "Saved inventory already active"
              }
              onClick={() => {
                setConfirmingActivation(true);
                setConfirmingRollback(false);
                setError("");
                setNotice("");
              }}
            >
              <Play size={16} /> Activate inventory
            </button>
          ) : null}
          {mutable ? (
            <button
              className="command-button"
              onClick={() => {
                setEditor("new");
                setError("");
                setNotice("");
              }}
            >
              <Plus size={16} /> Add policy
            </button>
          ) : null}
        </div>
      </header>
      <p className="scope-notice">
        Save and simulate freely, then activate the complete proxy, pool and
        policy inventory as one runtime revision.
      </p>
      {runtime ? (
        <section className="runtime-strip" aria-label="Active runtime">
          <div className="runtime-identity">
            <span className="eyebrow">Active runtime</span>
            <strong>Revision {runtime.revision}</strong>
            <span>
              {runtime.source === "configuration"
                ? "Started from configuration"
                : runtime.source === "rollback"
                  ? `Restored from revision ${runtime.source_revision}`
                  : "Activated inventory"}
            </span>
            <span
              className={
                runtime.staged_changes ? "runtime-pending" : "runtime-synced"
              }
            >
              {runtime.staged_changes
                ? "Staged changes pending activation"
                : "Saved inventory matches active routing"}
            </span>
          </div>
          <div className="runtime-counts" aria-label="Active resource counts">
            <span>{runtime.proxy_count} proxies</span>
            <span>{runtime.pool_count} pools</span>
            <span>{runtime.policy_count} policies</span>
          </div>
          {mutable &&
          history.some((item) => item.revision !== runtime.revision) ? (
            <div className="runtime-rollback">
              <select
                aria-label="Rollback revision"
                value={rollbackRevision}
                disabled={runtimeBusy}
                onChange={(event) => {
                  setRollbackRevision(event.target.value);
                  setConfirmingRollback(false);
                }}
              >
                <option value="">Select revision</option>
                {history
                  .filter((item) => item.revision !== runtime.revision)
                  .map((item) => (
                    <option key={item.revision} value={item.revision}>
                      Revision {item.revision}
                    </option>
                  ))}
              </select>
              <button
                className="pause-button secondary"
                disabled={!rollbackRevision || runtimeBusy}
                onClick={() => {
                  setConfirmingRollback(true);
                  setConfirmingActivation(false);
                  setError("");
                  setNotice("");
                }}
              >
                <RotateCcw size={15} /> Roll back
              </button>
            </div>
          ) : null}
        </section>
      ) : null}
      {confirmingActivation ? (
        <div
          className="inline-confirm runtime-confirm"
          role="group"
          aria-label="Activate saved inventory"
        >
          <span>
            This replaces live routing for new requests with all saved
            inventory.
          </span>
          <button
            className="command-button"
            disabled={runtimeBusy}
            onClick={() => void activate()}
          >
            <Play size={15} />
            {runtimeBusy ? "Activating…" : "Confirm activation"}
          </button>
          <button
            className="icon-button"
            aria-label="Cancel activation"
            disabled={runtimeBusy}
            onClick={() => setConfirmingActivation(false)}
          >
            <X size={14} />
          </button>
        </div>
      ) : null}
      {confirmingRollback ? (
        <div
          className="inline-confirm runtime-confirm"
          role="group"
          aria-label={`Confirm rollback to revision ${rollbackRevision}`}
        >
          <span>
            Restore revision {rollbackRevision} for new requests. Saved
            inventory remains staged and can be activated again.
          </span>
          <button
            className="command-button"
            disabled={runtimeBusy}
            onClick={() => void rollback()}
          >
            <RotateCcw size={15} />
            {runtimeBusy ? "Rolling back…" : "Confirm rollback"}
          </button>
          <button
            className="icon-button"
            aria-label="Cancel rollback"
            disabled={runtimeBusy}
            onClick={() => setConfirmingRollback(false)}
          >
            <X size={14} />
          </button>
        </div>
      ) : null}
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
        <PolicyEditor
          initial={editor === "new" ? undefined : editor}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onSaved={(record, created) => {
            setEditor(null);
            setSimulation(null);
            setRecords((items) =>
              created
                ? [...items, record].sort((a, b) =>
                    a.policy.id.localeCompare(b.policy.id),
                  )
                : items.map((item) =>
                    item.policy.id === record.policy.id ? record : item,
                  ),
            );
            setRuntime((current) =>
              current ? { ...current, staged_changes: true } : current,
            );
            setNotice(
              created
                ? `${record.policy.name} created and staged.`
                : "Policy updated and staged. Activate inventory when ready.",
            );
          }}
        />
      ) : null}
      {simulation ? (
        <section className="table-panel simulation-panel">
          <div className="section-header">
            <div>
              <h2>Simulation result</h2>
              <span>Simulation only · revision {simulation.revision}</span>
            </div>
            <button
              className="icon-button"
              aria-label="Close simulation"
              onClick={() => setSimulation(null)}
            >
              <X size={15} />
            </button>
          </div>
          <p>
            <strong>Outcome:</strong> {simulation.outcome}
          </p>
          <p>
            <strong>Matched rules:</strong>{" "}
            {(simulation.matched_rule_ids ?? []).join(", ") || "None"}
          </p>
          {(simulation.unknown_fields ?? []).length ? (
            <p>
              <strong>Unavailable fields:</strong>{" "}
              {(simulation.unknown_fields ?? []).join(", ")}
            </p>
          ) : null}
        </section>
      ) : null}
      <section className="table-panel" aria-label="Policy inventory">
        <div className="section-header">
          <div>
            <h2>Policy inventory</h2>
            <span>
              {records.length} loaded ·{" "}
              {mutable ? "operator controls" : "read only"}
            </span>
          </div>
        </div>
        {loading && records.length === 0 ? (
          <div className="empty-state" role="status">
            Loading policies…
          </div>
        ) : records.length === 0 ? (
          <div className="empty-state">
            <FileJson size={27} />
            <h3>No saved policies</h3>
            <p>
              {mutable
                ? "Create the policy required by your listeners before activating inventory."
                : "An operator has not created any policy inventory yet."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="policy-inventory-table">
              <thead>
                <tr>
                  <th>Policy</th>
                  <th>Rules</th>
                  <th>Revision</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {records.map((record) => (
                  <tr key={record.policy.id}>
                    <td className="policy-name-cell" data-label="Policy">
                      <strong>{record.policy.name}</strong>
                      <span>{record.policy.id}</span>
                    </td>
                    <td data-label="Rules">{record.policy.rules.length}</td>
                    <td data-label="Revision">{record.revision}</td>
                    {mutable ? (
                      <td className="policy-action-cell">
                        <div className="row-actions">
                          <button
                            className="icon-button policy-action-button"
                            aria-label={`Simulate ${record.policy.name}`}
                            title="Simulate policy"
                            onClick={() => void simulate(record)}
                          >
                            <FileJson size={14} />
                            <span>Simulate</span>
                          </button>
                          <button
                            className="icon-button policy-action-button"
                            aria-label={`Edit ${record.policy.name}`}
                            title="Edit policy"
                            onClick={() => {
                              setEditor(record);
                              setSimulation(null);
                            }}
                          >
                            <Edit3 size={14} />
                            <span>Edit</span>
                          </button>
                          <button
                            className="icon-button policy-action-button danger-icon"
                            aria-label={`Delete ${record.policy.name}`}
                            title="Delete policy"
                            onClick={() => {
                              setConfirming(record.policy.id);
                              setSimulation(null);
                              setError("");
                              setNotice("");
                            }}
                          >
                            <Trash2 size={14} />
                            <span>Delete</span>
                          </button>
                        </div>
                        {confirming === record.policy.id ? (
                          <div
                            className="inline-confirm"
                            role="group"
                            aria-label={`Delete ${record.policy.name}`}
                          >
                            <button
                              className="danger-button"
                              onClick={() => void remove(record)}
                            >
                              <Trash2 size={14} />
                              Confirm delete
                            </button>
                            <button
                              className="icon-button"
                              aria-label={`Cancel deleting ${record.policy.name}`}
                              onClick={() => setConfirming("")}
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ) : null}
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

function PolicyEditor({
  initial,
  onCancel,
  onExpired,
  onSaved,
}: {
  initial?: PolicyRecord | undefined;
  onCancel: () => void;
  onExpired: () => void;
  onSaved: (record: PolicyRecord, created: boolean) => void;
}) {
  const [text, setText] = useState(() =>
    JSON.stringify(initial?.policy ?? examplePolicy(), null, 2),
  );
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    let document: Policy;
    try {
      document = JSON.parse(text) as Policy;
    } catch {
      setError("Policy JSON is not valid.");
      setBusy(false);
      return;
    }
    try {
      const response = await api<PolicyResponse>(
        initial
          ? `/api/v1/policies/${encodeURIComponent(initial.policy.id)}`
          : "/api/v1/policies",
        {
          method: initial ? "PATCH" : "POST",
          body: initial
            ? { policy: document, revision: initial.revision }
            : { policy: document },
        },
      );
      onSaved(response.policy, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }
  return (
    <form className="resource-form policy-form" onSubmit={save}>
      <h2>{initial ? "Edit policy" : "Create policy"}</h2>
      <p className="field-help">
        Edit the canonical JSON document. The API validates rule fields, action
        types and pool references before saving.
      </p>
      <textarea
        aria-label="Policy JSON"
        value={text}
        onChange={(event) => setText(event.target.value)}
        spellCheck={false}
        rows={18}
      />
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create policy"}
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
