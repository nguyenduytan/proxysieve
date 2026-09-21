# Alerts and webhooks

ProxySieve alert rules match exact sanitized operational event types and dispatch
them to enabled HTTPS webhooks. Alert configuration and delivery history are stored
in SQLite; raw signing keys are not.

## Configure a signing key

Webhook `secret_ref` values use `secret://` references. ProxySieve maps each path
component to an uppercase environment variable separated by underscores. For
example:

```text
secret://webhooks/primary -> PROXYSIEVE_SECRET_WEBHOOKS_PRIMARY
```

Set the environment variable to the raw HMAC key before starting ProxySieve. The
receiver can verify `X-ProxySieve-Signature`, whose value is
`sha256=<hex HMAC-SHA256 body digest>`. Unsigned webhooks omit this header.

Every request also includes `X-ProxySieve-Event-ID` and `Idempotency-Key`, both set
to the operational event ID. The JSON body has a fixed version and sanitized event:

```json
{
  "version": 1,
  "event": {
    "id": "...",
    "at": "2026-09-21T00:00:00Z",
    "type": "source.refresh_failed",
    "severity": "error",
    "source": "scheduler",
    "target_type": "source",
    "target_id": "provider-a"
  }
}
```

## Delivery behavior

- Webhook URLs must use HTTPS and cannot contain credentials, query strings or fragments.
- DNS is resolved for every attempt; mixed or private address results are rejected.
- The checked address is pinned for the connection. Redirects and environment proxies are disabled.
- Network errors, timeouts, HTTP 429 and HTTP 5xx responses retry up to three attempts.
- Other HTTP failures are recorded without retry. Response bodies are discarded after a bounded read.
- Dispatcher queues are bounded. Drop, success, failure, queue and log-failure counters are exposed with the webhook list.
- Delivery history is globally capped at 10,000 newest rows and contains sanitized metadata only.

The Admin Panel `Alerts` workspace manages rules and webhooks, runs a test delivery,
and shows recent attempts. All alert endpoints require an administrator session;
mutations also require the session CSRF token.
