import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
  DatabaseZap,
  Pencil,
  Plus,
  Power,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import { api, ApiError, errorMessage } from "./api";
import type {
  ImportFormat,
  ProxySource,
  Role,
  SourcePage,
  SourceRecord,
  SourceRefreshResult,
} from "./api";

type Editor = "new" | SourceRecord | null;

export function SourceInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [records, setRecords] = useState<SourceRecord[]>([]);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editor, setEditor] = useState<Editor>(null);
  const [mutating, setMutating] = useState("");
  const [confirming, setConfirming] = useState("");
  const mutable = role !== "viewer";

  const load = useCallback(
    async (after = "", signal?: AbortSignal, preserveFeedback = false) => {
      setLoading(true);
      if (!preserveFeedback) {
        setError("");
        setNotice("");
      }
      try {
        const page = await api<SourcePage>(
          `/api/v1/sources?limit=100${after ? `&after=${encodeURIComponent(after)}` : ""}`,
          signal ? { signal } : {},
        );
        if (!signal?.aborted) {
          setRecords((previous) =>
            after ? [...previous, ...page.items] : page.items,
          );
          setNext(page.next_after);
        }
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
    void load("", controller.signal);
    return () => controller.abort();
  }, [load]);

  function replaceRecord(record: SourceRecord) {
    setRecords((current) =>
      current.map((item) =>
        item.source.id === record.source.id ? record : item,
      ),
    );
  }

  async function refresh(record: SourceRecord) {
    const id = record.source.id;
    setMutating(`${id}:refresh`);
    setConfirming("");
    setError("");
    setNotice("");
    try {
      const result = await api<SourceRefreshResult>(
        `/api/v1/sources/${encodeURIComponent(id)}/refresh`,
        {
          method: "POST",
          body: { revision: record.revision },
          timeoutMs: 25_000,
        },
      );
      replaceRecord(result.source);
      setNotice(
        `${record.source.name} refreshed: ${result.created} created, ${result.updated} updated, ${result.skipped} skipped, ${result.invalid} invalid.`,
      );
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else {
        const message = errorMessage(caught);
        await load("", undefined, true);
        setError(message);
      }
    } finally {
      setMutating("");
    }
  }

  async function toggle(record: SourceRecord) {
    const id = record.source.id;
    setMutating(`${id}:toggle`);
    setConfirming("");
    setError("");
    setNotice("");
    try {
      const result = await api<{ source: SourceRecord }>(
        `/api/v1/sources/${encodeURIComponent(id)}`,
        {
          method: "PATCH",
          body: {
            source: { ...record.source, enabled: !record.source.enabled },
            revision: record.revision,
          },
        },
      );
      replaceRecord(result.source);
      setNotice(
        `${record.source.name} ${result.source.source.enabled ? "enabled" : "disabled"}.`,
      );
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load("", undefined, true);
        setError(
          "This source changed elsewhere. The source list was refreshed.",
        );
      } else setError(errorMessage(caught));
    } finally {
      setMutating("");
    }
  }

  async function remove(record: SourceRecord) {
    const id = record.source.id;
    setMutating(`${id}:delete`);
    setError("");
    setNotice("");
    try {
      await api<void>(`/api/v1/sources/${encodeURIComponent(id)}`, {
        method: "DELETE",
        body: { revision: record.revision },
      });
      setRecords((current) => current.filter((item) => item.source.id !== id));
      setConfirming("");
      setNotice(`${record.source.name} deleted. Existing endpoints were kept.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409) {
        await load("", undefined, true);
        setError(
          "This source changed elsewhere. The source list was refreshed.",
        );
      } else setError(errorMessage(caught));
    } finally {
      setMutating("");
    }
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Proxy sources</h1>
          <p>Manage bounded HTTP feeds and local import files.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh source list"
            aria-label="Refresh source list"
            onClick={() => void load()}
            disabled={loading}
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
              Add source
            </button>
          ) : null}
        </div>
      </header>
      <p className="scope-notice">
        Source refreshes add or associate matching endpoints; they never remove
        existing endpoint records just because a feed changes or fails.
        Refreshed endpoints remain staged until runtime activation.
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
        <SourceForm
          initial={editor === "new" ? undefined : editor}
          onCancel={() => setEditor(null)}
          onExpired={onExpired}
          onConflict={(message) => {
            setEditor(null);
            void load("", undefined, true).then(() => setError(message));
          }}
          onSaved={(record, created) => {
            setRecords((current) =>
              created
                ? [...current, record].sort((left, right) =>
                    left.source.id.localeCompare(right.source.id),
                  )
                : current.map((item) =>
                    item.source.id === record.source.id ? record : item,
                  ),
            );
            setEditor(null);
            setError("");
            setConfirming("");
            setNotice(
              `${record.source.name} ${created ? "created" : "updated"}. ${scheduleLabel(record.source.refresh_interval_ns)}.`,
            );
          }}
        />
      ) : null}
      <section className="table-panel">
        <div className="section-header">
          <div>
            <h2>Configured feeds</h2>
            <span>
              {records.length} loaded{loading ? " · refreshing…" : ""}
            </span>
          </div>
        </div>
        {!loading && records.length === 0 ? (
          <div className="empty-state">
            <DatabaseZap size={27} />
            <h3>No proxy sources yet</h3>
            <p>
              {mutable
                ? "Add an HTTP feed or local import file, then refresh it to populate endpoint inventory."
                : "An operator can configure and schedule proxy feeds here."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="source-inventory-table">
              <thead>
                <tr>
                  <th>Source</th>
                  <th>Type</th>
                  <th>Schedule</th>
                  <th>Last refresh</th>
                  <th>Status</th>
                  <th>Revision</th>
                  {mutable ? <th>Actions</th> : null}
                </tr>
              </thead>
              <tbody>
                {records.map((record) => {
                  const { source, revision } = record;
                  const busy = mutating.startsWith(`${source.id}:`);
                  const refreshing = mutating === `${source.id}:refresh`;
                  const deleting = mutating === `${source.id}:delete`;
                  const unsupported =
                    source.type !== "api" && source.type !== "file";
                  return (
                    <tr key={source.id}>
                      <td className="source-name-cell" data-label="Source">
                        <strong>{source.name}</strong>
                        <span>{sourceHost(source)}</span>
                      </td>
                      <td data-label="Type">{source.type.toUpperCase()}</td>
                      <td data-label="Schedule">
                        {scheduleLabel(source.refresh_interval_ns)}
                      </td>
                      <td data-label="Last refresh">
                        {formatRefreshTime(source.last_refresh_at)}
                      </td>
                      <td data-label="Status">
                        <span
                          className={statusClass(source.last_refresh_status)}
                        >
                          {source.last_refresh_status || "Never refreshed"}
                        </span>
                      </td>
                      <td data-label="Revision">{revision}</td>
                      {mutable ? (
                        <td className="source-action-cell">
                          {confirming === source.id ? (
                            <div
                              className="inline-confirm"
                              role="group"
                              aria-label={`Delete ${source.name}`}
                            >
                              <button
                                className="danger-button"
                                disabled={busy}
                                onClick={() => void remove(record)}
                              >
                                <Trash2 size={14} />
                                {deleting ? "Deleting…" : "Confirm delete"}
                              </button>
                              <button
                                className="icon-button"
                                aria-label={`Cancel deleting ${source.name}`}
                                disabled={busy}
                                onClick={() => setConfirming("")}
                              >
                                <X size={14} />
                              </button>
                            </div>
                          ) : (
                            <div className="row-actions">
                              <button
                                className="icon-button source-action-button"
                                title="Refresh now"
                                aria-label={`Refresh ${source.name}`}
                                disabled={
                                  busy || unsupported || !source.enabled
                                }
                                onClick={() => void refresh(record)}
                              >
                                <RefreshCw size={14} />
                                <span>
                                  {refreshing ? "Refreshing…" : "Refresh"}
                                </span>
                              </button>
                              {!unsupported ? (
                                <button
                                  className="icon-button source-action-button"
                                  title="Edit source"
                                  aria-label={`Edit ${source.name}`}
                                  disabled={busy}
                                  onClick={() => {
                                    setEditor(record);
                                    setConfirming("");
                                    setError("");
                                    setNotice("");
                                  }}
                                >
                                  <Pencil size={14} />
                                  <span>Edit</span>
                                </button>
                              ) : null}
                              <button
                                className="icon-button source-action-button"
                                title={
                                  source.enabled
                                    ? "Disable source"
                                    : "Enable source"
                                }
                                aria-label={`${source.enabled ? "Disable" : "Enable"} ${source.name}`}
                                disabled={busy}
                                onClick={() => void toggle(record)}
                              >
                                <Power size={14} />
                                <span>
                                  {source.enabled ? "Disable" : "Enable"}
                                </span>
                              </button>
                              <button
                                className="icon-button source-action-button danger-icon"
                                title="Delete source"
                                aria-label={`Delete ${source.name}`}
                                disabled={busy}
                                onClick={() => {
                                  setConfirming(source.id);
                                  setEditor(null);
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
        {next ? (
          <div className="section-header">
            <button
              className="pause-button"
              disabled={loading}
              onClick={() => void load(next)}
            >
              Load more
            </button>
          </div>
        ) : null}
      </section>
    </div>
  );
}

function SourceForm({
  initial,
  onSaved,
  onCancel,
  onExpired,
  onConflict,
}: {
  initial?: SourceRecord | undefined;
  onSaved: (record: SourceRecord, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
  onConflict: (message: string) => void;
}) {
  const [name, setName] = useState(initial?.source.name ?? "");
  const [sourceType, setSourceType] = useState<"api" | "file">(
    initial?.source.type === "file" ? "file" : "api",
  );
  const [location, setLocation] = useState(
    initial?.source.type === "file"
      ? (initial.source.config?.path ?? "")
      : (initial?.source.config?.url ?? ""),
  );
  const [format, setFormat] = useState<ImportFormat>(
    parseFormat(initial?.source.config?.format),
  );
  const [itemsField, setItemsField] = useState(
    initial?.source.config?.items_field ?? "",
  );
  const [endpointField, setEndpointField] = useState(
    initial?.source.config?.endpoint_field ?? "",
  );
  const [protocolField, setProtocolField] = useState(
    initial?.source.config?.protocol_field ?? "",
  );
  const [hostField, setHostField] = useState(
    initial?.source.config?.host_field ?? "",
  );
  const [portField, setPortField] = useState(
    initial?.source.config?.port_field ?? "",
  );
  const [minutes, setMinutes] = useState(
    String(
      (initial?.source.refresh_interval_ns ?? 3_600_000_000_000) /
        60_000_000_000,
    ),
  );
  const [enabled, setEnabled] = useState(initial?.source.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (
      (sourceType === "api" && !validSourceURL(location)) ||
      (sourceType === "file" && !validSourcePath(location))
    ) {
      setError(
        sourceType === "api"
          ? "Use an absolute HTTP or HTTPS URL without embedded credentials."
          : "Use a relative file path inside the ProxySieve imports directory.",
      );
      return;
    }
    const interval = Number(minutes);
    if (!Number.isFinite(interval) || interval < 0 || interval > 43_200) {
      setError("Refresh interval must be between 0 and 43,200 minutes.");
      return;
    }
    setBusy(true);
    const config: Record<string, string> = {
      [sourceType === "api" ? "url" : "path"]: location.trim(),
      format,
    };
    if (format !== "text") {
      if (endpointField.trim()) config.endpoint_field = endpointField.trim();
      if (protocolField.trim()) config.protocol_field = protocolField.trim();
      if (hostField.trim()) config.host_field = hostField.trim();
      if (portField.trim()) config.port_field = portField.trim();
      if (format === "json" && itemsField.trim())
        config.items_field = itemsField.trim();
    }
    const source: ProxySource = initial
      ? {
          ...initial.source,
          name,
          type: sourceType,
          config,
          refresh_interval_ns: interval * 60_000_000_000,
          enabled,
        }
      : {
          id: "",
          name,
          type: sourceType,
          config,
          refresh_interval_ns: interval * 60_000_000_000,
          enabled,
        };
    try {
      const response = await api<{ source: SourceRecord }>(
        initial
          ? `/api/v1/sources/${encodeURIComponent(initial.source.id)}`
          : "/api/v1/sources",
        {
          method: initial ? "PATCH" : "POST",
          body: initial
            ? { source, revision: initial.revision }
            : { source: { ...source, id: undefined } },
        },
      );
      onSaved(response.source, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409)
        onConflict(
          "This source changed elsewhere. The source list was refreshed.",
        );
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit proxy source" : "Add proxy source"}</h2>
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
          Refresh interval (minutes)
          <input
            required
            type="number"
            min={0}
            max={43_200}
            step={1}
            value={minutes}
            onChange={(event) => setMinutes(event.target.value)}
          />
        </label>
        <label>
          Source type
          <select
            value={sourceType}
            onChange={(event) => {
              setSourceType(event.target.value as "api" | "file");
              setLocation("");
            }}
          >
            <option value="api">HTTP API</option>
            <option value="file">Local file</option>
          </select>
        </label>
        <label>
          Input format
          <select
            value={format}
            onChange={(event) => setFormat(event.target.value as ImportFormat)}
          >
            <option value="text">Text lines</option>
            <option value="csv">CSV records</option>
            <option value="json">JSON records</option>
          </select>
        </label>
        <label className="form-span">
          {sourceType === "api"
            ? "HTTP or HTTPS feed URL"
            : "Path inside the imports directory"}
          <input
            required
            type={sourceType === "api" ? "url" : "text"}
            placeholder={
              sourceType === "api"
                ? "https://feeds.example.invalid/proxies.json"
                : `provider/proxies.${format === "text" ? "txt" : format}`
            }
            value={location}
            onChange={(event) => setLocation(event.target.value)}
          />
        </label>
        {format !== "text" ? (
          <details className="form-span">
            <summary>Field mapping</summary>
            <div className="form-grid">
              {format === "json" ? (
                <label>
                  JSON array field
                  <input
                    maxLength={256}
                    placeholder="items"
                    value={itemsField}
                    onChange={(event) => setItemsField(event.target.value)}
                  />
                </label>
              ) : null}
              <label>
                Endpoint field
                <input
                  maxLength={256}
                  placeholder="endpoint"
                  value={endpointField}
                  onChange={(event) => setEndpointField(event.target.value)}
                />
              </label>
              <label>
                Protocol field
                <input
                  maxLength={256}
                  placeholder="protocol"
                  value={protocolField}
                  onChange={(event) => setProtocolField(event.target.value)}
                />
              </label>
              <label>
                Host field
                <input
                  maxLength={256}
                  placeholder="host"
                  value={hostField}
                  onChange={(event) => setHostField(event.target.value)}
                />
              </label>
              <label>
                Port field
                <input
                  maxLength={256}
                  placeholder="port"
                  value={portField}
                  onChange={(event) => setPortField(event.target.value)}
                />
              </label>
            </div>
          </details>
        ) : null}
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Enable automatic and manual refresh
        </label>
      </div>
      <p className="field-help">
        Use 0 minutes for manual-only refresh. HTTP sources reject private
        destinations, redirects, and embedded credentials. File sources stay
        inside the data directory's imports folder.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create source"}
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

function validSourceURL(raw: string): boolean {
  try {
    const parsed = new URL(raw);
    return (
      (parsed.protocol === "http:" || parsed.protocol === "https:") &&
      parsed.hostname !== "" &&
      parsed.username === "" &&
      parsed.password === ""
    );
  } catch {
    return false;
  }
}

function sourceHost(source: ProxySource): string {
  if (source.type === "file") return source.config?.path || "Invalid file path";
  if (source.type !== "api") return "Managed externally";
  try {
    return new URL(source.config?.url ?? "").host || "Invalid URL";
  } catch {
    return "Invalid URL";
  }
}

function validSourcePath(raw: string): boolean {
  const value = raw.trim().replaceAll("\\", "/");
  return (
    value !== "" &&
    !value.startsWith("/") &&
    !/^[a-z]:\//i.test(value) &&
    !value.split("/").includes("..")
  );
}

function parseFormat(value: string | undefined): ImportFormat {
  return value === "csv" || value === "json" ? value : "text";
}

function scheduleLabel(nanoseconds: number): string {
  if (!Number.isFinite(nanoseconds) || nanoseconds <= 0) return "Manual only";
  const minutes = nanoseconds / 60_000_000_000;
  if (minutes < 60) return `Every ${minutes} min`;
  const hours = minutes / 60;
  if (hours < 24) return `Every ${hours} hr`;
  return `Every ${hours / 24} day`;
}

function formatRefreshTime(value: string | undefined): string {
  if (!value || value.startsWith("0001-")) return "Never";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "Unknown"
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(date);
}

function statusClass(status: string | undefined): string {
  if (!status) return "source-status neutral";
  return status.startsWith("ok:")
    ? "source-status success"
    : "source-status failed";
}
