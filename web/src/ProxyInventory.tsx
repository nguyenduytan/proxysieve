import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Globe2, Plus, RefreshCw } from "lucide-react";
import { api, ApiError, errorMessage } from "./api";
import type { EndpointRecord, ProxyPage, Role, Preview } from "./api";

export function ProxyInventory({
  role,
  onExpired,
}: {
  role: Role;
  onExpired: () => void;
}) {
  const [records, setRecords] = useState<EndpointRecord[]>([]);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [form, setForm] = useState<"none" | "create" | "preview">("none");
  const [notice, setNotice] = useState("");
  const load = useCallback(
    async (after = "", signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const page = await api<ProxyPage>(
          `/api/v1/proxies?limit=100${after ? `&after=${encodeURIComponent(after)}` : ""}`,
          signal ? { signal } : {},
        );
        if (!signal?.aborted) {
          setRecords((previous) =>
            after ? [...previous, ...page.items] : page.items,
          );
          setNext(page.next_after);
        }
      } catch (error) {
        if (signal?.aborted) return;
        if (error instanceof ApiError && error.status === 401) onExpired();
        else setError(errorMessage(error));
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
  const mutable = role !== "viewer";
  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Proxy inventory</h1>
          <p>
            Saved endpoint metadata. Live routing still uses the configured YAML
            pools and policies.
          </p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh inventory"
            aria-label="Refresh inventory"
            onClick={() => void load()}
            disabled={loading}
          >
            <RefreshCw size={17} />
          </button>
          {mutable && (
            <>
              <button
                className="pause-button secondary"
                onClick={() => setForm(form === "preview" ? "none" : "preview")}
              >
                Preview import
              </button>
              <button
                className="command-button"
                onClick={() => setForm(form === "create" ? "none" : "create")}
              >
                <Plus size={16} />
                Add proxy
              </button>
            </>
          )}
        </div>
      </header>
      <p className="scope-notice">
        Saving here does not activate a route, including after restart. To use
        an endpoint now, define its proxy, pool membership and policy in your
        configuration file.
      </p>
      {error && (
        <div role="alert" className="auth-error">
          {error}
        </div>
      )}
      {notice && (
        <div role="status" className="success-notice">
          {notice}
        </div>
      )}
      {form === "create" && (
        <CreateProxy
          onSaved={() => {
            setForm("none");
            setNotice(
              "Proxy saved to inventory. Runtime routing has not changed.",
            );
            void load();
          }}
          onCancel={() => setForm("none")}
          onExpired={onExpired}
        />
      )}
      {form === "preview" && (
        <ImportPreview onCancel={() => setForm("none")} onExpired={onExpired} />
      )}
      <section className="table-panel">
        <div className="section-header">
          <div>
            <h2>Saved endpoints</h2>
            <span>
              {records.length} loaded{loading ? " · refreshing…" : ""}
            </span>
          </div>
        </div>
        {!loading && records.length === 0 ? (
          <div className="empty-state">
            <Globe2 size={27} />
            <h3>Your proxy inventory is empty</h3>
            <p>
              {mutable
                ? "Add a proxy's host, protocol and port. Store credentials separately with a secret reference."
                : "An operator can add endpoint metadata here."}
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Protocol</th>
                  <th>Endpoint</th>
                  <th>Credential reference</th>
                  <th>Enabled metadata</th>
                  <th>Revision</th>
                </tr>
              </thead>
              <tbody>
                {records.map(({ endpoint, revision }) => (
                  <tr key={endpoint.id}>
                    <td className="host-cell">{endpoint.name}</td>
                    <td>{endpoint.protocol.toUpperCase()}</td>
                    <td className="mono">
                      {endpoint.host}:{endpoint.port}
                    </td>
                    <td>{endpoint.credential_ref || "None"}</td>
                    <td>{endpoint.enabled ? "Yes" : "No"}</td>
                    <td>{revision}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {next && (
          <div className="section-header">
            <button
              className="pause-button"
              disabled={loading}
              onClick={() => void load(next)}
            >
              Load more
            </button>
          </div>
        )}
      </section>
    </div>
  );
}
function CreateProxy({
  onSaved,
  onCancel,
  onExpired,
}: {
  onSaved: () => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [name, setName] = useState(""),
    [host, setHost] = useState(""),
    [protocol, setProtocol] = useState("http"),
    [port, setPort] = useState("8080"),
    [reference, setReference] = useState("");
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/api/v1/proxies", {
        method: "POST",
        body: {
          endpoint: {
            name,
            protocol,
            host,
            port: Number(port),
            enabled: true,
            ...(reference ? { credential_ref: reference } : {}),
          },
        },
      });
      onSaved();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onExpired();
      else setError(errorMessage(error));
      setBusy(false);
    }
  }
  return (
    <form className="resource-form" onSubmit={save}>
      <h2>Add endpoint metadata</h2>
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
          Protocol
          <select
            value={protocol}
            onChange={(event) => setProtocol(event.target.value)}
          >
            {["http", "https", "socks5", "socks5h"].map((value) => (
              <option key={value} value={value}>
                {value.toUpperCase()}
              </option>
            ))}
          </select>
        </label>
        <label>
          Host
          <input
            required
            placeholder="proxy.example.invalid"
            value={host}
            onChange={(event) => setHost(event.target.value)}
          />
        </label>
        <label>
          Port
          <input
            required
            type="number"
            min={1}
            max={65535}
            value={port}
            onChange={(event) => setPort(event.target.value)}
          />
        </label>
        <label>
          Credential reference (optional)
          <input
            placeholder="secret://upstream/auth"
            value={reference}
            onChange={(event) => setReference(event.target.value)}
          />
        </label>
      </div>
      <p className="field-help">
        Do not put a username or password in the host field. Credentials are
        resolved separately by the gateway.
      </p>
      {error && (
        <p role="alert" className="auth-error">
          {error}
        </p>
      )}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : "Save to inventory"}
        </button>
        <button
          type="button"
          className="pause-button secondary"
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
function ImportPreview({
  onCancel,
  onExpired,
}: {
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [input, setInput] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [result, setResult] = useState<Preview | null>(null);
  async function preview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      setResult(
        await api<Preview>("/api/v1/proxies/import/preview", {
          method: "POST",
          body: { input },
        }),
      );
      setInput("");
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) onExpired();
      else setError(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="resource-form" onSubmit={preview}>
      <h2>Preview proxy input</h2>
      <p className="field-help">
        Preview only. Nothing is saved. Current deduplication compares
        addresses, not credential identities. Credential import is not
        implemented; use test input only.
      </p>
      <label>
        Proxy list
        <textarea
          required
          rows={5}
          maxLength={48_000}
          placeholder="http://proxy.example.invalid:8080"
          value={input}
          onChange={(event) => setInput(event.target.value)}
        />
      </label>
      {error && (
        <p role="alert" className="auth-error">
          {error}
        </p>
      )}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Parsing…" : "Preview input"}
        </button>
        <button
          type="button"
          className="pause-button secondary"
          onClick={onCancel}
        >
          Close preview
        </button>
      </div>
      {result && (
        <div className="preview-result" role="status">
          <h3>
            {result.valid} valid · {result.invalid} invalid ·{" "}
            {result.duplicates} duplicate addresses
          </h3>
          {result.items.slice(0, 20).map((item) => (
            <p key={item.line}>
              Line {item.line}:{" "}
              {item.error
                ? item.error
                : `${item.result.endpoint.protocol}://${item.result.endpoint.host}:${item.result.endpoint.port}`}
            </p>
          ))}
        </div>
      )}
    </form>
  );
}
