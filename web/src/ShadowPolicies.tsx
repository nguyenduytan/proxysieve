import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Activity, Edit3, Plus, RefreshCw, Trash2, X } from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type {
  PolicyRecord,
  Role,
  ShadowComparison,
  ShadowPage,
  ShadowRecord,
} from "./api";

export function ShadowPolicies({
  policies,
  role,
  onExpired,
  onInteraction,
}: {
  policies: PolicyRecord[];
  role: Role;
  onExpired: () => void;
  onInteraction: () => void;
}) {
  const [records, setRecords] = useState<ShadowRecord[]>([]);
  const [comparisons, setComparisons] = useState<
    Record<string, ShadowComparison>
  >({});
  const [editor, setEditor] = useState<ShadowRecord | "new" | null>(null);
  const [confirming, setConfirming] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const mutable = role !== "viewer";

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const page = await api<ShadowPage>(
          "/api/v1/shadow",
          signal ? { signal } : {},
        );
        const results = await Promise.all(
          page.items.map(
            async (record) =>
              [
                record.shadow.id,
                await api<ShadowComparison>(
                  `/api/v1/shadow/${encodeURIComponent(record.shadow.id)}/comparison`,
                  signal ? { signal } : {},
                ),
              ] as const,
          ),
        );
        if (!signal?.aborted) {
          setRecords(page.items);
          setComparisons(Object.fromEntries(results));
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

  async function remove(record: ShadowRecord) {
    onInteraction();
    try {
      await api<void>(
        `/api/v1/shadow/${encodeURIComponent(record.shadow.id)}`,
        { method: "DELETE", body: { revision: record.revision } },
      );
      setRecords((items) =>
        items.filter((item) => item.shadow.id !== record.shadow.id),
      );
      setComparisons((current) => {
        const next = { ...current };
        delete next[record.shadow.id];
        return next;
      });
      setConfirming("");
      setNotice(`${record.shadow.name} deleted.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    }
  }

  return (
    <section className="table-panel shadow-panel" aria-label="Shadow policies">
      <div className="section-header">
        <div>
          <h2>Shadow comparison</h2>
          <span>
            {records.length} configured · simulation only · counters reset on
            restart
          </span>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh comparisons"
            aria-label="Refresh shadow comparisons"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={16} />
          </button>
          {mutable ? (
            <button
              className="command-button"
              disabled={policies.length === 0}
              onClick={() => {
                onInteraction();
                setEditor("new");
                setError("");
                setNotice("");
              }}
            >
              <Plus size={16} /> Add shadow
            </button>
          ) : null}
        </div>
      </div>
      {error && !editor ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      {notice && !editor ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      {editor ? (
        <ShadowEditor
          initial={editor === "new" ? undefined : editor}
          policies={policies}
          onExpired={onExpired}
          onCancel={() => setEditor(null)}
          onSaved={(record, created) => {
            setEditor(null);
            setRecords((items) =>
              created
                ? [...items, record].sort((a, b) =>
                    a.shadow.name.localeCompare(b.shadow.name),
                  )
                : items.map((item) =>
                    item.shadow.id === record.shadow.id ? record : item,
                  ),
            );
            setComparisons((current) => ({
              ...current,
              [record.shadow.id]: emptyComparison(record),
            }));
            setNotice(
              created
                ? `${record.shadow.name} created.`
                : `${record.shadow.name} updated; comparison counters reset.`,
            );
          }}
        />
      ) : null}
      {loading && records.length === 0 ? (
        <div className="empty-state" role="status">
          Loading shadow policies…
        </div>
      ) : records.length === 0 ? (
        <div className="empty-state">
          <Activity size={27} />
          <h3>No shadow comparison configured</h3>
          <p>
            {mutable
              ? "Compare a saved candidate against an active policy without changing live routing."
              : "An operator has not configured a shadow policy."}
          </p>
        </div>
      ) : (
        <div className="table-scroll">
          <table className="shadow-table">
            <thead>
              <tr>
                <th>Comparison</th>
                <th>State</th>
                <th>Samples</th>
                <th>Different</th>
                <th>Pool changes</th>
                <th>Average eval</th>
                {mutable ? <th>Actions</th> : null}
              </tr>
            </thead>
            <tbody>
              {records.map((record) => {
                const comparison = comparisons[record.shadow.id];
                return (
                  <tr key={record.shadow.id}>
                    <td className="shadow-name-cell" data-label="Comparison">
                      <strong>{record.shadow.name}</strong>
                      <span>
                        {record.shadow.active_policy_id} →{" "}
                        {record.shadow.policy.id}
                      </span>
                    </td>
                    <td data-label="State">
                      {record.shadow.enabled ? "Enabled" : "Paused"}
                    </td>
                    <td data-label="Samples">{comparison?.samples ?? 0}</td>
                    <td data-label="Different">
                      {comparison?.different_decisions ?? 0}
                    </td>
                    <td data-label="Pool changes">
                      {comparison?.pool_differences ?? 0}
                    </td>
                    <td data-label="Average eval">
                      {formatDuration(comparison?.average_evaluation_ns ?? 0)}
                    </td>
                    {mutable ? (
                      <td className="shadow-action-cell">
                        <div className="row-actions">
                          <button
                            className="icon-button shadow-action-button"
                            title="Edit shadow comparison"
                            aria-label={`Edit ${record.shadow.name}`}
                            onClick={() => {
                              onInteraction();
                              setEditor(record);
                              setError("");
                              setNotice("");
                            }}
                          >
                            <Edit3 size={14} />
                            <span>Edit</span>
                          </button>
                          <button
                            className="icon-button shadow-action-button danger-icon"
                            title="Delete shadow comparison"
                            aria-label={`Delete ${record.shadow.name}`}
                            onClick={() => setConfirming(record.shadow.id)}
                          >
                            <Trash2 size={14} />
                            <span>Delete</span>
                          </button>
                        </div>
                        {confirming === record.shadow.id ? (
                          <div
                            className="inline-confirm"
                            role="group"
                            aria-label={`Delete ${record.shadow.name}`}
                          >
                            <button
                              className="danger-button"
                              onClick={() => void remove(record)}
                            >
                              <Trash2 size={14} /> Confirm delete
                            </button>
                            <button
                              className="icon-button"
                              aria-label={`Cancel deleting ${record.shadow.name}`}
                              onClick={() => setConfirming("")}
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ) : null}
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
  );
}

function ShadowEditor({
  initial,
  policies,
  onCancel,
  onExpired,
  onSaved,
}: {
  initial?: ShadowRecord | undefined;
  policies: PolicyRecord[];
  onCancel: () => void;
  onExpired: () => void;
  onSaved: (record: ShadowRecord, created: boolean) => void;
}) {
  const [name, setName] = useState(initial?.shadow.name ?? "Candidate policy");
  const [activeID, setActiveID] = useState(
    initial?.shadow.active_policy_id ?? policies[0]?.policy.id ?? "",
  );
  const [candidateID, setCandidateID] = useState(
    initial?.shadow.policy.id ?? policies[0]?.policy.id ?? "",
  );
  const [enabled, setEnabled] = useState(initial?.shadow.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const candidate = policies.find((item) => item.policy.id === candidateID);
    if (!candidate) {
      setError("Select a saved candidate policy.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const shadow = {
        id: initial?.shadow.id ?? "",
        name,
        active_policy_id: activeID,
        policy: candidate.policy,
        enabled,
      };
      const record = await api<ShadowRecord>(
        initial
          ? `/api/v1/shadow/${encodeURIComponent(initial.shadow.id)}`
          : "/api/v1/shadow",
        {
          method: initial ? "PATCH" : "POST",
          body: initial ? { shadow, revision: initial.revision } : { shadow },
        },
      );
      onSaved(record, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h3>{initial ? "Edit shadow comparison" : "Add shadow comparison"}</h3>
      <div className="form-grid">
        <label>
          Name
          <input
            required
            maxLength={256}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          Active policy
          <select
            required
            value={activeID}
            onChange={(event) => setActiveID(event.target.value)}
          >
            {policies.map((record) => (
              <option key={record.policy.id} value={record.policy.id}>
                {record.policy.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Candidate snapshot
          <select
            required
            value={candidateID}
            onChange={(event) => setCandidateID(event.target.value)}
          >
            {policies.map((record) => (
              <option key={record.policy.id} value={record.policy.id}>
                {record.policy.name} · revision {record.revision}
              </option>
            ))}
          </select>
        </label>
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Evaluate eligible requests
        </label>
      </div>
      <p className="field-help">
        The candidate is copied at save time. It records aggregate decisions and
        never opens an upstream connection or changes the live route.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create shadow"}
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

function emptyComparison(record: ShadowRecord): ShadowComparison {
  return {
    shadow_id: record.shadow.id,
    active_policy_id: record.shadow.active_policy_id,
    shadow_policy_id: record.shadow.policy.id,
    samples: 0,
    same_decisions: 0,
    different_decisions: 0,
    pool_differences: 0,
    evaluation_errors: 0,
    estimated_upstream_bytes: 0,
    average_evaluation_ns: 0,
    active_decisions: {},
    shadow_decisions: {},
  };
}

function formatDuration(value: number) {
  return value > 0 ? `${(value / 1e6).toFixed(2)} ms` : "—";
}
