import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  KeyRound,
  Pencil,
  Plus,
  Power,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import { api, ApiError, errorMessage } from "./api";
import type {
  APIKeyCreation,
  APIKeyPage,
  APIKeyRecord,
  ClientPage,
  ClientRecord,
} from "./api";

type Editor = "new" | ClientRecord | null;

interface OneTimeKey {
  clientName: string;
  key: APIKeyRecord;
  token: string;
}

export function ClientAccess({ onExpired }: { onExpired: () => void }) {
  const [clients, setClients] = useState<ClientRecord[]>([]);
  const [next, setNext] = useState("");
  const [keys, setKeys] = useState<Record<string, APIKeyRecord[]>>({});
  const [expanded, setExpanded] = useState("");
  const [loading, setLoading] = useState(true);
  const [keysLoading, setKeysLoading] = useState("");
  const [mutating, setMutating] = useState("");
  const [confirming, setConfirming] = useState("");
  const [editor, setEditor] = useState<Editor>(null);
  const [secret, setSecret] = useState<OneTimeKey | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const clearFeedback = useCallback(() => {
    setError("");
    setNotice("");
  }, []);

  const load = useCallback(
    async (after = "", signal?: AbortSignal, preserveFeedback = false) => {
      setLoading(true);
      if (!preserveFeedback) clearFeedback();
      try {
        const page = await api<ClientPage>(
          `/api/v1/clients?limit=100${after ? `&after=${encodeURIComponent(after)}` : ""}`,
          signal ? { signal } : {},
        );
        if (!signal?.aborted) {
          setClients((current) =>
            after ? [...current, ...page.items] : page.items,
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
    [clearFeedback, onExpired],
  );

  const loadKeys = useCallback(
    async (clientID: string) => {
      setKeysLoading(clientID);
      try {
        const page = await api<APIKeyPage>(
          `/api/v1/clients/${encodeURIComponent(clientID)}/api-keys`,
        );
        setKeys((current) => ({ ...current, [clientID]: page.items }));
      } catch (caught) {
        if (caught instanceof ApiError && caught.status === 401) onExpired();
        else setError(errorMessage(caught));
      } finally {
        setKeysLoading("");
      }
    },
    [onExpired],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load("", controller.signal);
    return () => controller.abort();
  }, [load]);

  async function toggleKeys(client: ClientRecord) {
    clearFeedback();
    setConfirming("");
    if (expanded === client.id) {
      setExpanded("");
      return;
    }
    setExpanded(client.id);
    if (keys[client.id] === undefined) await loadKeys(client.id);
  }

  async function toggleClient(client: ClientRecord) {
    setMutating(`${client.id}:toggle`);
    setConfirming("");
    clearFeedback();
    const { revision, ...document } = client;
    try {
      const updated = await api<ClientRecord>(
        `/api/v1/clients/${encodeURIComponent(client.id)}`,
        {
          method: "PATCH",
          body: {
            client: { ...document, enabled: !client.enabled },
            revision,
          },
        },
      );
      replaceClient(updated);
      setNotice(`${client.name} ${updated.enabled ? "enabled" : "disabled"}.`);
    } catch (caught) {
      await handleMutationError(caught);
    } finally {
      setMutating("");
    }
  }

  async function createKey(client: ClientRecord) {
    if (secret) return;
    setMutating(`${client.id}:key`);
    setConfirming("");
    clearFeedback();
    try {
      const created = await api<APIKeyCreation>(
        `/api/v1/clients/${encodeURIComponent(client.id)}/api-keys`,
        { method: "POST", body: {} },
      );
      setKeys((current) => ({
        ...current,
        [client.id]: [
          created.api_key,
          ...(current[client.id] ?? []).filter(
            (key) => key.id !== created.api_key.id,
          ),
        ],
      }));
      setExpanded(client.id);
      setCopied(false);
      setSecret({
        clientName: client.name,
        key: created.api_key,
        token: created.token,
      });
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setMutating("");
    }
  }

  async function revokeKey(client: ClientRecord, key: APIKeyRecord) {
    setMutating(`${client.id}:revoke:${key.id}`);
    clearFeedback();
    try {
      await api<void>(
        `/api/v1/clients/${encodeURIComponent(client.id)}/api-keys/${encodeURIComponent(key.id)}`,
        { method: "DELETE", body: {} },
      );
      setKeys((current) => ({
        ...current,
        [client.id]: (current[client.id] ?? []).map((item) =>
          item.id === key.id
            ? { ...item, revoked_at: new Date().toISOString() }
            : item,
        ),
      }));
      setConfirming("");
      setNotice(`API key ${key.prefix}… revoked for ${client.name}.`);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    } finally {
      setMutating("");
    }
  }

  async function removeClient(client: ClientRecord) {
    setMutating(`${client.id}:delete`);
    clearFeedback();
    try {
      await api<void>(`/api/v1/clients/${encodeURIComponent(client.id)}`, {
        method: "DELETE",
        body: { revision: client.revision },
      });
      setClients((current) => current.filter((item) => item.id !== client.id));
      setKeys((current) => {
        const updated = { ...current };
        delete updated[client.id];
        return updated;
      });
      if (expanded === client.id) setExpanded("");
      if (secret?.key.client_id === client.id) setSecret(null);
      setConfirming("");
      setNotice(`${client.name} and its API keys were deleted.`);
    } catch (caught) {
      await handleMutationError(caught);
    } finally {
      setMutating("");
    }
  }

  async function handleMutationError(caught: unknown) {
    if (caught instanceof ApiError && caught.status === 401) {
      onExpired();
      return;
    }
    if (caught instanceof ApiError && caught.status === 409) {
      await load("", undefined, true);
      setEditor(null);
      setError("This client changed elsewhere. The client list was refreshed.");
      return;
    }
    setError(errorMessage(caught));
  }

  function replaceClient(client: ClientRecord) {
    setClients((current) =>
      current.map((item) => (item.id === client.id ? client : item)),
    );
  }

  async function copyToken() {
    if (!secret) return;
    try {
      if (!navigator.clipboard)
        throw new Error("Clipboard access is unavailable.");
      await navigator.clipboard.writeText(secret.token);
      setCopied(true);
      setError("");
    } catch {
      setCopied(false);
      setError(
        "Could not copy automatically. Select and copy the token manually.",
      );
    }
  }

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Clients &amp; API keys</h1>
          <p>Manage downstream identities and one-time bearer credentials.</p>
        </div>
        <div className="table-actions">
          <button
            className="icon-button"
            title="Refresh clients"
            aria-label="Refresh clients"
            disabled={loading}
            onClick={() => void load()}
          >
            <RefreshCw size={17} />
          </button>
          <button
            className="command-button"
            onClick={() => {
              setEditor(editor === "new" ? null : "new");
              setConfirming("");
              clearFeedback();
            }}
          >
            <Plus size={16} />
            Add client
          </button>
        </div>
      </header>
      <p className="scope-notice">
        Raw API keys are displayed once after creation and are never stored in
        plaintext. Revoking or deleting access takes effect for new
        authentications immediately.
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
      {secret ? (
        <section className="one-time-key" aria-labelledby="one-time-key-title">
          <div>
            <KeyRound size={19} />
            <div>
              <h2 id="one-time-key-title">Save this API key now</h2>
              <p>
                Key {secret.key.prefix}… authenticates {secret.clientName}. It
                cannot be displayed again after this panel is dismissed.
              </p>
            </div>
          </div>
          <textarea
            aria-label="One-time API key"
            readOnly
            rows={2}
            value={secret.token}
            onFocus={(event) => event.currentTarget.select()}
          />
          <div className="table-actions">
            <button className="command-button" onClick={() => void copyToken()}>
              {copied ? <Check size={15} /> : <Copy size={15} />}
              {copied ? "Copied" : "Copy key"}
            </button>
            <button
              className="pause-button secondary"
              onClick={() => {
                setSecret(null);
                setCopied(false);
              }}
            >
              I saved it
            </button>
          </div>
        </section>
      ) : null}
      {editor ? (
        <ClientForm
          initial={editor === "new" ? undefined : editor}
          onExpired={onExpired}
          onCancel={() => setEditor(null)}
          onConflict={() =>
            void load("", undefined, true).then(() => {
              setEditor(null);
              setError(
                "This client changed elsewhere. The client list was refreshed.",
              );
            })
          }
          onSaved={(client, created) => {
            setClients((current) =>
              created
                ? [...current, client].sort((left, right) =>
                    left.id.localeCompare(right.id),
                  )
                : current.map((item) =>
                    item.id === client.id ? client : item,
                  ),
            );
            setEditor(null);
            setConfirming("");
            setError("");
            setNotice(`${client.name} ${created ? "created" : "updated"}.`);
          }}
        />
      ) : null}
      <section className="table-panel client-panel">
        <div className="section-header">
          <div>
            <h2>Downstream clients</h2>
            <span>
              {clients.length} loaded{loading ? " · refreshing…" : ""}
            </span>
          </div>
        </div>
        {!loading && clients.length === 0 ? (
          <div className="empty-state">
            <KeyRound size={27} />
            <h3>No downstream clients yet</h3>
            <p>
              Create a logical client, then generate an API key for a listener
              configured with API-key authentication.
            </p>
          </div>
        ) : (
          <div className="client-grid">
            {clients.map((client) => {
              const clientKeys = keys[client.id];
              const isExpanded = expanded === client.id;
              const busy = mutating.startsWith(`${client.id}:`);
              const deleting = mutating === `${client.id}:delete`;
              return (
                <article className="client-card" key={client.id}>
                  <header>
                    <div>
                      <strong>{client.name}</strong>
                      <span className="mono">{client.id}</span>
                    </div>
                    <span
                      className={`client-state ${client.enabled ? "enabled" : "disabled"}`}
                    >
                      {client.enabled ? "Enabled" : "Disabled"}
                    </span>
                  </header>
                  <dl className="client-facts">
                    <div>
                      <dt>Authentication</dt>
                      <dd>Bearer API key</dd>
                    </div>
                    <div>
                      <dt>Routing scope</dt>
                      <dd>{scopeLabel(client)}</dd>
                    </div>
                    <div>
                      <dt>Last seen</dt>
                      <dd>{formatClientTime(client.last_seen_at)}</dd>
                    </div>
                    <div>
                      <dt>Revision</dt>
                      <dd>{client.revision}</dd>
                    </div>
                  </dl>
                  {confirming === `client:${client.id}` ? (
                    <div
                      className="delete-client-confirm"
                      role="group"
                      aria-label={`Delete ${client.name}`}
                    >
                      <p>Delete this client and every API key issued to it?</p>
                      <div className="table-actions">
                        <button
                          className="danger-button"
                          disabled={busy}
                          onClick={() => void removeClient(client)}
                        >
                          <Trash2 size={14} />
                          {deleting ? "Deleting…" : "Confirm delete"}
                        </button>
                        <button
                          className="icon-button"
                          aria-label={`Cancel deleting ${client.name}`}
                          disabled={busy}
                          onClick={() => setConfirming("")}
                        >
                          <X size={14} />
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="client-actions">
                      <button
                        className="pause-button secondary"
                        aria-expanded={isExpanded}
                        disabled={busy}
                        onClick={() => void toggleKeys(client)}
                      >
                        {isExpanded ? (
                          <ChevronUp size={14} />
                        ) : (
                          <ChevronDown size={14} />
                        )}
                        {isExpanded ? "Hide keys" : "Manage keys"}
                      </button>
                      <button
                        className="icon-button"
                        title="Edit client"
                        aria-label={`Edit ${client.name}`}
                        disabled={busy}
                        onClick={() => {
                          setEditor(client);
                          setConfirming("");
                          clearFeedback();
                        }}
                      >
                        <Pencil size={14} />
                      </button>
                      <button
                        className="icon-button"
                        title={
                          client.enabled ? "Disable client" : "Enable client"
                        }
                        aria-label={`${client.enabled ? "Disable" : "Enable"} ${client.name}`}
                        disabled={busy}
                        onClick={() => void toggleClient(client)}
                      >
                        <Power size={14} />
                      </button>
                      <button
                        className="icon-button danger-icon"
                        title="Delete client"
                        aria-label={`Delete ${client.name}`}
                        disabled={busy}
                        onClick={() => {
                          setConfirming(`client:${client.id}`);
                          setEditor(null);
                          clearFeedback();
                        }}
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  )}
                  {isExpanded ? (
                    <KeyList
                      client={client}
                      items={clientKeys}
                      loading={keysLoading === client.id}
                      mutating={mutating}
                      confirming={confirming}
                      createBlocked={secret !== null}
                      onCreate={() => void createKey(client)}
                      onConfirm={setConfirming}
                      onRevoke={(key) => void revokeKey(client, key)}
                    />
                  ) : null}
                </article>
              );
            })}
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

function KeyList({
  client,
  items,
  loading,
  mutating,
  confirming,
  createBlocked,
  onCreate,
  onConfirm,
  onRevoke,
}: {
  client: ClientRecord;
  items: APIKeyRecord[] | undefined;
  loading: boolean;
  mutating: string;
  confirming: string;
  createBlocked: boolean;
  onCreate: () => void;
  onConfirm: (value: string) => void;
  onRevoke: (key: APIKeyRecord) => void;
}) {
  return (
    <section className="key-list" aria-label={`API keys for ${client.name}`}>
      <header>
        <div>
          <h3>API keys</h3>
          <span>Only prefixes and lifecycle metadata are retained.</span>
        </div>
        <button
          className="command-button compact"
          disabled={loading || createBlocked || mutating !== ""}
          title={
            createBlocked
              ? "Save or dismiss the currently displayed one-time key first"
              : "Create API key"
          }
          onClick={onCreate}
        >
          <Plus size={14} />
          New key
        </button>
      </header>
      {loading ? (
        <p className="key-empty" role="status">
          Loading API keys…
        </p>
      ) : !items?.length ? (
        <p className="key-empty">No API keys issued to this client.</p>
      ) : (
        <div className="key-items">
          {items.map((key) => {
            const revoked = Boolean(key.revoked_at);
            const confirmID = `key:${key.id}`;
            const busy = mutating === `${client.id}:revoke:${key.id}`;
            return (
              <div className="key-item" key={key.id}>
                <div>
                  <strong className="mono">{key.prefix}…</strong>
                  <span>Created {formatClientTime(key.created_at)}</span>
                </div>
                <span
                  className={`client-state ${revoked ? "disabled" : "enabled"}`}
                >
                  {revoked ? "Revoked" : "Active"}
                </span>
                {!revoked ? (
                  confirming === confirmID ? (
                    <div className="inline-confirm">
                      <button
                        className="danger-button"
                        disabled={busy}
                        onClick={() => onRevoke(key)}
                      >
                        {busy ? "Revoking…" : "Confirm revoke"}
                      </button>
                      <button
                        className="icon-button"
                        aria-label={`Cancel revoking ${key.prefix}`}
                        disabled={busy}
                        onClick={() => onConfirm("")}
                      >
                        <X size={14} />
                      </button>
                    </div>
                  ) : (
                    <button
                      className="pause-button secondary key-revoke"
                      disabled={mutating !== ""}
                      onClick={() => onConfirm(confirmID)}
                    >
                      Revoke
                    </button>
                  )
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function ClientForm({
  initial,
  onSaved,
  onCancel,
  onExpired,
  onConflict,
}: {
  initial?: ClientRecord | undefined;
  onSaved: (client: ClientRecord, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
  onConflict: () => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [enabled, setEnabled] = useState(initial?.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalizedName = name.trim();
    setError("");
    if (!normalizedName || normalizedName.length > 256) {
      setError("Client name must contain between 1 and 256 characters.");
      return;
    }
    setBusy(true);
    try {
      const client = initial
        ? await updateClient(initial, normalizedName, enabled)
        : await api<ClientRecord>("/api/v1/clients", {
            method: "POST",
            body: { name: normalizedName },
          });
      onSaved(client, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else if (caught instanceof ApiError && caught.status === 409)
        onConflict();
      else setError(errorMessage(caught));
      setBusy(false);
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit downstream client" : "Add downstream client"}</h2>
      <div className="form-grid">
        <label>
          Client name
          <input
            required
            autoFocus
            maxLength={256}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label>
          Authentication
          <input readOnly value="Bearer API key" />
        </label>
        {initial ? (
          <label className="checkbox-field">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(event) => setEnabled(event.target.checked)}
            />
            Allow this client to authenticate
          </label>
        ) : null}
      </div>
      <p className="field-help">
        Listener, pool, policy, IP and budget assignments remain unchanged when
        renaming or enabling this client.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create client"}
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

async function updateClient(
  initial: ClientRecord,
  name: string,
  enabled: boolean,
): Promise<ClientRecord> {
  const { revision, ...client } = initial;
  return api<ClientRecord>(
    `/api/v1/clients/${encodeURIComponent(initial.id)}`,
    {
      method: "PATCH",
      body: { client: { ...client, name, enabled }, revision },
    },
  );
}

function scopeLabel(client: ClientRecord): string {
  const assignments =
    (client.allowed_listeners?.length ?? 0) +
    (client.allowed_pools?.length ?? 0) +
    (client.policy_ids?.length ?? 0) +
    (client.budget_ids?.length ?? 0) +
    (client.ip_allowlist?.length ?? 0);
  return assignments === 0
    ? "Default configuration"
    : `${assignments} explicit assignment${assignments === 1 ? "" : "s"}`;
}

function formatClientTime(value: string | undefined): string {
  if (!value || value.startsWith("0001-")) return "Never";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "Unknown"
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(date);
}
