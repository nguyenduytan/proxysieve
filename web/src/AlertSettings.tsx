import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import {
  BellRing,
  History,
  Pencil,
  Plus,
  RefreshCw,
  Send,
  Trash2,
  Webhook as WebhookIcon,
  X,
} from "lucide-react";
import { ApiError, api, errorMessage } from "./api";
import type {
  AlertDeliveryStats,
  AlertRule,
  AlertRulePage,
  AlertRuleRecord,
  WebhookConfig,
  WebhookDelivery,
  WebhookDeliveryPage,
  WebhookPage,
  WebhookRecord,
} from "./api";

type Editor =
  | { kind: "rule"; record?: AlertRuleRecord }
  | { kind: "webhook"; record?: WebhookRecord };

const emptyStats: AlertDeliveryStats = {
  events_dropped: 0,
  deliveries_dropped: 0,
  succeeded: 0,
  failed: 0,
  log_failures: 0,
  queued_events: 0,
  queued_deliveries: 0,
};

export function parseEventTypes(value: string): string[] {
  return Array.from(
    new Set(
      value
        .split(/[,\n]/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  );
}

export function AlertSettings({ onExpired }: { onExpired: () => void }) {
  const [rules, setRules] = useState<AlertRuleRecord[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookRecord[]>([]);
  const [stats, setStats] = useState(emptyStats);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [confirming, setConfirming] = useState("");
  const [testing, setTesting] = useState("");
  const [deliveryWebhook, setDeliveryWebhook] = useState("");
  const [deliveries, setDeliveries] = useState<WebhookDelivery[]>([]);
  const [loadingDeliveries, setLoadingDeliveries] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const fail = useCallback(
    (caught: unknown) => {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else setError(errorMessage(caught));
    },
    [onExpired],
  );

  const load = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError("");
      try {
        const options = signal ? { signal } : {};
        const [rulePage, webhookPage] = await Promise.all([
          api<AlertRulePage>("/api/v1/alerts", options),
          api<WebhookPage>("/api/v1/webhooks", options),
        ]);
        if (signal?.aborted) return;
        setRules(sortRules(rulePage.items));
        setWebhooks(sortWebhooks(webhookPage.items));
        setStats(webhookPage.delivery);
      } catch (caught) {
        if (!signal?.aborted) fail(caught);
      } finally {
        if (!signal?.aborted) setLoading(false);
      }
    },
    [fail],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  async function loadDeliveries(id: string) {
    setLoadingDeliveries(true);
    setDeliveryWebhook(id);
    setError("");
    try {
      const page = await api<WebhookDeliveryPage>(
        `/api/v1/webhooks/${encodeURIComponent(id)}/deliveries?limit=100`,
      );
      setDeliveries(page.items);
    } catch (caught) {
      fail(caught);
    } finally {
      setLoadingDeliveries(false);
    }
  }

  async function testWebhook(record: WebhookRecord) {
    setTesting(record.webhook.id);
    setError("");
    setNotice("");
    try {
      const result = await api<{ delivery: WebhookDelivery }>(
        `/api/v1/webhooks/${encodeURIComponent(record.webhook.id)}/test`,
        { method: "POST", timeoutMs: 60_000 },
      );
      setNotice(
        result.delivery.success
          ? `${record.webhook.name} returned HTTP ${result.delivery.status_code}.`
          : `${record.webhook.name} test failed: ${result.delivery.error_code}.`,
      );
      if (deliveryWebhook === record.webhook.id)
        await loadDeliveries(record.webhook.id);
      await load();
    } catch (caught) {
      fail(caught);
    } finally {
      setTesting("");
    }
  }

  async function remove(
    kind: "rule" | "webhook",
    id: string,
    revision: number,
  ) {
    setError("");
    setNotice("");
    try {
      await api<void>(
        `/api/v1/${kind === "rule" ? "alerts" : "webhooks"}/${encodeURIComponent(id)}`,
        { method: "DELETE", body: { revision } },
      );
      if (kind === "rule")
        setRules((current) =>
          current.filter((record) => record.rule.id !== id),
        );
      else {
        setWebhooks((current) =>
          current.filter((record) => record.webhook.id !== id),
        );
        if (deliveryWebhook === id) {
          setDeliveryWebhook("");
          setDeliveries([]);
        }
      }
      setConfirming("");
      setNotice(`${kind === "rule" ? "Alert rule" : "Webhook"} deleted.`);
    } catch (caught) {
      fail(caught);
    }
  }

  function open(next: Editor) {
    setEditor(next);
    setConfirming("");
    setError("");
    setNotice("");
  }

  const selectedWebhook = webhooks.find(
    ({ webhook }) => webhook.id === deliveryWebhook,
  );

  return (
    <div className="content">
      <header className="page-heading overview-heading">
        <div>
          <h1>Alerts</h1>
          <p>Route exact operational events to signed HTTPS webhooks.</p>
        </div>
        <button
          className="icon-button"
          title="Refresh alerts"
          aria-label="Refresh alerts"
          disabled={loading}
          onClick={() => void load()}
        >
          <RefreshCw size={17} />
        </button>
      </header>
      <p className="scope-notice">
        Webhook URLs must use HTTPS. Signing keys stay in environment-backed
        secret references and are never stored in alert configuration.
      </p>
      <dl className="alert-runtime" aria-label="Webhook delivery status">
        <div>
          <dt>Succeeded</dt>
          <dd>{stats.succeeded}</dd>
        </div>
        <div>
          <dt>Failed</dt>
          <dd>{stats.failed}</dd>
        </div>
        <div>
          <dt>Queued</dt>
          <dd>{stats.queued_events + stats.queued_deliveries}</dd>
        </div>
        <div>
          <dt>Dropped</dt>
          <dd>{stats.events_dropped + stats.deliveries_dropped}</dd>
        </div>
        <div>
          <dt>Log failures</dt>
          <dd>{stats.log_failures}</dd>
        </div>
      </dl>
      {error ? (
        <div role="alert" className="auth-error">
          {error}
        </div>
      ) : null}
      {notice ? (
        <div role="status" className="success-notice">
          {notice}
        </div>
      ) : null}
      {editor?.kind === "webhook" ? (
        <WebhookForm
          {...(editor.record ? { initial: editor.record } : {})}
          onExpired={onExpired}
          onCancel={() => setEditor(null)}
          onSaved={(saved, created) => {
            setWebhooks((current) =>
              sortWebhooks(
                created
                  ? [...current, saved]
                  : current.map((item) =>
                      item.webhook.id === saved.webhook.id ? saved : item,
                    ),
              ),
            );
            setEditor(null);
            setNotice(`Webhook ${created ? "created" : "updated"}.`);
          }}
        />
      ) : null}
      {editor?.kind === "rule" ? (
        <RuleForm
          {...(editor.record ? { initial: editor.record } : {})}
          webhooks={webhooks}
          onExpired={onExpired}
          onCancel={() => setEditor(null)}
          onSaved={(saved, created) => {
            setRules((current) =>
              sortRules(
                created
                  ? [...current, saved]
                  : current.map((item) =>
                      item.rule.id === saved.rule.id ? saved : item,
                    ),
              ),
            );
            setEditor(null);
            setNotice(`Alert rule ${created ? "created" : "updated"}.`);
          }}
        />
      ) : null}
      <section className="table-panel alert-section">
        <div className="section-header">
          <div>
            <h2>Alert rules</h2>
            <span>{rules.length} configured</span>
          </div>
          <button
            className="command-button"
            disabled={webhooks.length === 0}
            title={webhooks.length === 0 ? "Add a webhook first" : "Add rule"}
            onClick={() => open({ kind: "rule" })}
          >
            <Plus size={16} />
            Add rule
          </button>
        </div>
        {loading && rules.length === 0 ? (
          <div className="empty-state" role="status">
            Loading alert rules…
          </div>
        ) : rules.length === 0 ? (
          <div className="empty-state">
            <BellRing size={27} />
            <h3>No alert rules</h3>
            <p>
              Add a webhook, then choose the operational event types to send.
            </p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="alert-table">
              <thead>
                <tr>
                  <th>Rule</th>
                  <th>Event types</th>
                  <th>Webhooks</th>
                  <th>Status</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {rules.map((record) => (
                  <tr key={record.rule.id}>
                    <td className="alert-name-cell">
                      <strong>{record.rule.name}</strong>
                      <span>{record.rule.id}</span>
                    </td>
                    <td data-label="Event types" className="alert-list-cell">
                      {record.rule.event_types.join(", ")}
                    </td>
                    <td data-label="Webhooks" className="alert-list-cell">
                      {record.rule.webhook_ids
                        .map(
                          (id) =>
                            webhooks.find((item) => item.webhook.id === id)
                              ?.webhook.name ?? id,
                        )
                        .join(", ")}
                    </td>
                    <td data-label="Status">
                      <span
                        className={`client-state ${record.rule.enabled ? "enabled" : "disabled"}`}
                      >
                        {record.rule.enabled ? "Enabled" : "Disabled"}
                      </span>
                    </td>
                    <td className="alert-action-cell">
                      {confirming === `rule:${record.rule.id}` ? (
                        <div className="inline-confirm">
                          <button
                            className="danger-button"
                            onClick={() =>
                              void remove(
                                "rule",
                                record.rule.id,
                                record.revision,
                              )
                            }
                          >
                            Confirm delete
                          </button>
                          <button
                            className="icon-button"
                            aria-label={`Cancel deleting ${record.rule.name}`}
                            onClick={() => setConfirming("")}
                          >
                            <X size={14} />
                          </button>
                        </div>
                      ) : (
                        <div className="row-actions">
                          <button
                            className="icon-button"
                            title="Edit alert rule"
                            aria-label={`Edit ${record.rule.name}`}
                            onClick={() => open({ kind: "rule", record })}
                          >
                            <Pencil size={14} />
                          </button>
                          <button
                            className="icon-button danger-icon"
                            title="Delete alert rule"
                            aria-label={`Delete ${record.rule.name}`}
                            onClick={() =>
                              setConfirming(`rule:${record.rule.id}`)
                            }
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <section className="table-panel alert-section">
        <div className="section-header">
          <div>
            <h2>Webhook destinations</h2>
            <span>{webhooks.length} configured</span>
          </div>
          <button
            className="command-button"
            onClick={() => open({ kind: "webhook" })}
          >
            <Plus size={16} />
            Add webhook
          </button>
        </div>
        {loading && webhooks.length === 0 ? (
          <div className="empty-state" role="status">
            Loading webhooks…
          </div>
        ) : webhooks.length === 0 ? (
          <div className="empty-state">
            <WebhookIcon size={27} />
            <h3>No webhook destinations</h3>
            <p>Add an HTTPS endpoint before creating alert rules.</p>
          </div>
        ) : (
          <div className="table-scroll">
            <table className="alert-table webhook-table">
              <thead>
                <tr>
                  <th>Webhook</th>
                  <th>Destination</th>
                  <th>Signing</th>
                  <th>Status</th>
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {webhooks.map((record) => {
                  const inUse = rules.some((item) =>
                    item.rule.webhook_ids.includes(record.webhook.id),
                  );
                  return (
                    <tr key={record.webhook.id}>
                      <td className="alert-name-cell">
                        <strong>{record.webhook.name}</strong>
                        <span>{record.webhook.id}</span>
                      </td>
                      <td data-label="Destination" className="mono alert-url">
                        {record.webhook.url}
                      </td>
                      <td data-label="Signing">
                        {record.webhook.secret_ref ? "HMAC-SHA256" : "Unsigned"}
                      </td>
                      <td data-label="Status">
                        <span
                          className={`client-state ${record.webhook.enabled ? "enabled" : "disabled"}`}
                        >
                          {record.webhook.enabled ? "Enabled" : "Disabled"}
                        </span>
                      </td>
                      <td className="alert-action-cell">
                        {confirming === `webhook:${record.webhook.id}` ? (
                          <div className="inline-confirm">
                            <button
                              className="danger-button"
                              onClick={() =>
                                void remove(
                                  "webhook",
                                  record.webhook.id,
                                  record.revision,
                                )
                              }
                            >
                              Confirm delete
                            </button>
                            <button
                              className="icon-button"
                              aria-label={`Cancel deleting ${record.webhook.name}`}
                              onClick={() => setConfirming("")}
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ) : (
                          <div className="row-actions">
                            <button
                              className="icon-button"
                              title="Test webhook"
                              aria-label={`Test ${record.webhook.name}`}
                              disabled={testing === record.webhook.id}
                              onClick={() => void testWebhook(record)}
                            >
                              <Send size={14} />
                            </button>
                            <button
                              className="icon-button"
                              title="View deliveries"
                              aria-label={`View ${record.webhook.name} deliveries`}
                              onClick={() =>
                                void loadDeliveries(record.webhook.id)
                              }
                            >
                              <History size={14} />
                            </button>
                            <button
                              className="icon-button"
                              title="Edit webhook"
                              aria-label={`Edit ${record.webhook.name}`}
                              onClick={() => open({ kind: "webhook", record })}
                            >
                              <Pencil size={14} />
                            </button>
                            <button
                              className="icon-button danger-icon"
                              title={
                                inUse
                                  ? "Remove this webhook from alert rules first"
                                  : "Delete webhook"
                              }
                              aria-label={`Delete ${record.webhook.name}`}
                              disabled={inUse}
                              onClick={() =>
                                setConfirming(`webhook:${record.webhook.id}`)
                              }
                            >
                              <Trash2 size={14} />
                            </button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
      {deliveryWebhook ? (
        <section className="table-panel alert-section">
          <div className="section-header">
            <div>
              <h2>Recent deliveries</h2>
              <span>{selectedWebhook?.webhook.name ?? deliveryWebhook}</span>
            </div>
            <button
              className="icon-button"
              title="Close delivery history"
              aria-label="Close delivery history"
              onClick={() => {
                setDeliveryWebhook("");
                setDeliveries([]);
              }}
            >
              <X size={16} />
            </button>
          </div>
          {loadingDeliveries ? (
            <div className="empty-state" role="status">
              Loading deliveries…
            </div>
          ) : deliveries.length === 0 ? (
            <div className="empty-state">
              <History size={27} />
              <h3>No delivery attempts</h3>
              <p>Test this webhook or wait for a matching alert event.</p>
            </div>
          ) : (
            <div className="table-scroll">
              <table className="delivery-table">
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Result</th>
                    <th>Attempt</th>
                    <th>HTTP</th>
                    <th>Error</th>
                    <th>Duration</th>
                    <th>Event ID</th>
                  </tr>
                </thead>
                <tbody>
                  {deliveries.map((delivery) => (
                    <tr key={delivery.id}>
                      <td className="mono" data-label="Time">
                        {formatTime(delivery.attempted_at)}
                      </td>
                      <td data-label="Result">
                        <span
                          className={`client-state ${delivery.success ? "enabled" : "disabled"}`}
                        >
                          {delivery.success ? "Success" : "Failed"}
                        </span>
                      </td>
                      <td data-label="Attempt">{delivery.attempt}</td>
                      <td data-label="HTTP">{delivery.status_code || "—"}</td>
                      <td data-label="Error">{delivery.error_code || "—"}</td>
                      <td data-label="Duration">
                        {formatDuration(delivery.duration_ns)}
                      </td>
                      <td className="mono" data-label="Event ID">
                        {delivery.event_id}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      ) : null}
    </div>
  );
}

function WebhookForm({
  initial,
  onSaved,
  onCancel,
  onExpired,
}: {
  initial?: WebhookRecord;
  onSaved: (record: WebhookRecord, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [id, setID] = useState(initial?.webhook.id ?? "");
  const [name, setName] = useState(initial?.webhook.name ?? "");
  const [url, setURL] = useState(initial?.webhook.url ?? "");
  const [secretRef, setSecretRef] = useState(initial?.webhook.secret_ref ?? "");
  const [enabled, setEnabled] = useState(initial?.webhook.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    let target: URL;
    try {
      target = new URL(url);
    } catch {
      setError("Enter a valid HTTPS webhook URL.");
      return;
    }
    if (
      target.protocol !== "https:" ||
      target.username !== "" ||
      target.password !== "" ||
      target.search !== "" ||
      target.hash !== ""
    ) {
      setError(
        "Webhook URL must use HTTPS without credentials, query or fragment.",
      );
      return;
    }
    if (
      secretRef &&
      !/^secret:\/\/[A-Za-z0-9_-]+(?:\/[A-Za-z0-9_-]+){0,7}$/.test(secretRef)
    ) {
      setError("Signing secret must be a secret:// reference.");
      return;
    }
    setBusy(true);
    const webhook: WebhookConfig = {
      id,
      name,
      url,
      ...(secretRef ? { secret_ref: secretRef } : {}),
      enabled,
    };
    try {
      const record = await api<WebhookRecord>(
        initial
          ? `/api/v1/webhooks/${encodeURIComponent(id)}`
          : "/api/v1/webhooks",
        {
          method: initial ? "PATCH" : "POST",
          body: initial ? { webhook, revision: initial.revision } : { webhook },
        },
      );
      onSaved(record, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else {
        setError(errorMessage(caught));
        setBusy(false);
      }
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit webhook" : "Add webhook"}</h2>
      <div className="form-grid">
        <label>
          ID
          <input
            required
            autoFocus
            disabled={Boolean(initial)}
            maxLength={128}
            pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,127}"
            value={id}
            onChange={(event) => setID(event.target.value)}
          />
        </label>
        <label>
          Name
          <input
            required
            maxLength={128}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="form-span">
          HTTPS URL
          <input
            required
            type="url"
            maxLength={2048}
            placeholder="https://alerts.example/hooks/proxysieve"
            value={url}
            onChange={(event) => setURL(event.target.value)}
          />
        </label>
        <label className="form-span">
          Signing secret reference (optional)
          <input
            maxLength={256}
            placeholder="secret://webhooks/primary"
            value={secretRef}
            onChange={(event) => setSecretRef(event.target.value)}
          />
        </label>
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Deliver matching events to this webhook
        </label>
      </div>
      <p className="field-help">
        Optional signatures use HMAC-SHA256 in X-ProxySieve-Signature. Define
        the referenced value in the ProxySieve environment.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create webhook"}
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

function RuleForm({
  initial,
  webhooks,
  onSaved,
  onCancel,
  onExpired,
}: {
  initial?: AlertRuleRecord;
  webhooks: WebhookRecord[];
  onSaved: (record: AlertRuleRecord, created: boolean) => void;
  onCancel: () => void;
  onExpired: () => void;
}) {
  const [id, setID] = useState(initial?.rule.id ?? "");
  const [name, setName] = useState(initial?.rule.name ?? "");
  const [eventTypes, setEventTypes] = useState(
    initial?.rule.event_types.join("\n") ?? "source.refresh_failed",
  );
  const [webhookIDs, setWebhookIDs] = useState(initial?.rule.webhook_ids ?? []);
  const [enabled, setEnabled] = useState(initial?.rule.enabled ?? true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    const types = parseEventTypes(eventTypes);
    if (
      types.length === 0 ||
      types.some((value) => !/^[a-z][a-z0-9_.-]{1,127}$/.test(value))
    ) {
      setError(
        "Enter valid exact event types, one per line or comma-separated.",
      );
      return;
    }
    if (webhookIDs.length === 0) {
      setError("Select at least one webhook destination.");
      return;
    }
    setBusy(true);
    const rule: AlertRule = {
      id,
      name,
      event_types: types,
      webhook_ids: webhookIDs,
      enabled,
    };
    try {
      const record = await api<AlertRuleRecord>(
        initial ? `/api/v1/alerts/${encodeURIComponent(id)}` : "/api/v1/alerts",
        {
          method: initial ? "PATCH" : "POST",
          body: initial ? { rule, revision: initial.revision } : { rule },
        },
      );
      onSaved(record, !initial);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) onExpired();
      else {
        setError(errorMessage(caught));
        setBusy(false);
      }
    }
  }

  return (
    <form className="resource-form" onSubmit={save}>
      <h2>{initial ? "Edit alert rule" : "Add alert rule"}</h2>
      <div className="form-grid">
        <label>
          ID
          <input
            required
            autoFocus
            disabled={Boolean(initial)}
            maxLength={128}
            pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,127}"
            value={id}
            onChange={(event) => setID(event.target.value)}
          />
        </label>
        <label>
          Name
          <input
            required
            maxLength={128}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="form-span">
          Exact event types
          <textarea
            required
            rows={4}
            placeholder={"source.refresh_failed\nbudget.exhausted"}
            value={eventTypes}
            onChange={(event) => setEventTypes(event.target.value)}
          />
        </label>
        <fieldset className="choice-group form-span">
          <legend>Webhook destinations</legend>
          <div className="choice-grid">
            {webhooks.map(({ webhook }) => (
              <label key={webhook.id}>
                <input
                  type="checkbox"
                  checked={webhookIDs.includes(webhook.id)}
                  onChange={(event) =>
                    setWebhookIDs((current) =>
                      event.target.checked
                        ? [...current, webhook.id]
                        : current.filter((id) => id !== webhook.id),
                    )
                  }
                />
                <span>
                  <strong>{webhook.name}</strong>
                  <small>{webhook.url}</small>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        <label className="checkbox-field">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => setEnabled(event.target.checked)}
          />
          Match and dispatch this rule
        </label>
      </div>
      <p className="field-help">
        Rules use exact event names. A webhook selected by multiple matching
        rules receives the event once.
      </p>
      {error ? (
        <p role="alert" className="auth-error">
          {error}
        </p>
      ) : null}
      <div className="table-actions">
        <button className="command-button" disabled={busy}>
          {busy ? "Saving…" : initial ? "Save changes" : "Create rule"}
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

function sortRules(items: AlertRuleRecord[]): AlertRuleRecord[] {
  return [...items].sort((left, right) =>
    left.rule.name.localeCompare(right.rule.name),
  );
}

function sortWebhooks(items: WebhookRecord[]): WebhookRecord[] {
  return [...items].sort((left, right) =>
    left.webhook.name.localeCompare(right.webhook.name),
  );
}

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "Unknown"
    : new Intl.DateTimeFormat(undefined, {
        dateStyle: "medium",
        timeStyle: "medium",
      }).format(date);
}

function formatDuration(value: number): string {
  return Number.isFinite(value) && value >= 0
    ? `${(value / 1_000_000).toFixed(1)} ms`
    : "—";
}
