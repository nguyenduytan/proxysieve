# Admin API Foundation

The local control plane binds to `127.0.0.1:9090` by default. It is an HTTP API
foundation, not yet the full documented v1 control plane or dashboard server.

Available endpoints:

```text
GET  /health
GET  /ready
GET  /api/v1/auth/setup-status
POST /api/v1/auth/setup
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/me
GET  /api/v1/openapi.yaml
GET  /api/v1/system/info
GET  /api/v1/traffic/live
GET  /api/v1/traffic/history
GET  /api/v1/traffic/summary
GET  /api/v1/traffic/timeseries
GET  /api/v1/audit
GET  /api/v1/proxies
POST /api/v1/proxies
POST /api/v1/proxies/import/preview
POST /api/v1/proxies/import
GET  /api/v1/proxies/{id}
PATCH /api/v1/proxies/{id}
DELETE /api/v1/proxies/{id}
GET  /api/v1/clients
POST /api/v1/clients
GET  /api/v1/clients/{id}/api-keys
POST /api/v1/clients/{id}/api-keys
DELETE /api/v1/clients/{id}/api-keys/{key-id}
```

The first run creates no default password. A random setup token appears only on
the start command's standard output while no admin user exists. Submit it once to
`POST /api/v1/auth/setup` with an administrator username and a password of at least
12 characters. The API never returns the setup token.

Passwords use Argon2id with calibrated fixed parameters stored in their encoded
hash. Admin sessions are opaque, HttpOnly, SameSite=Strict cookies held in memory
for 12 hours. Restarting the process invalidates them. Cookie-authenticated mutation
routes require the non-HttpOnly CSRF cookie's value in `X-CSRF-Token`.

The current role hierarchy is admin > operator > viewer. Existing read endpoints
require viewer; admin may use them. Future mutation endpoints define their own
minimum role. Raw traffic credentials and stored secret values never appear in
these response models.

Admin host requests must be `localhost`, `127.0.0.1`, or `::1`; other Host values
are rejected. Responses set restrictive security headers and `Cache-Control: no-store`.
Remote/TLS admin serving rejects explicitly until the TLS secret/certificate path is
implemented. Do not work around this by exposing the local admin port publicly.

`GET /api/v1/traffic/live` is authenticated and returns the bounded newest-event
gateway buffer plus asynchronous persistence health. `GET /api/v1/traffic/history`
returns recent SQLite events. `traffic/summary` and `traffic/timeseries` return
bounded analytics over aligned UTC ranges with optional client, pool, proxy,
action and protocol filters. Timeseries accepts minute/hour/day granularity and
returns at most 2,000 non-empty buckets. These application-stream measurements do
not represent network-interface bytes or a provider invoice. SSE and domain/rule
breakdowns arrive in later milestones. `configured_costs` is grouped by currency
and includes the rated upstream-byte coverage; these values are configured
estimates, not provider-billed amounts.

The embedded Admin Panel deliberately lists only API-backed destinations:
Overview, Traffic, Proxies and System. Planned workspaces such as Alerts are not
rendered as disabled navigation. This avoids duplicate or inert menu surfaces
while features are still under development.

Proxy inventory updates and deletes use an optimistic `revision` precondition. Send
the complete endpoint document with its current revision to `PATCH`, or the current
revision alone to `DELETE`; stale revisions return `409 PROXY_CONFLICT`. These
mutations remain inventory-only and require an operator session plus CSRF token.

`POST /api/v1/proxies/import` commits a parser-approved text import in one storage
transaction. Its optional `mode` is `skip` (default), `update`, or `create`; malformed
lines are ignored after validation and the response reports created, updated and
skipped counts. The import never persists credentials embedded in source text.

## Client API Keys

An admin may create an enabled logical client with `POST /api/v1/clients`, then
create a downstream key with `POST /api/v1/clients/{id}/api-keys`. The raw key is
returned only by its creation response. SQLite retains a SHA-256 hash and display
prefix, never the raw token. Administrators can list clients and key metadata, and
revoke an existing key with the CSRF-protected DELETE route. A machine-readable
contract is available at `GET /api/v1/openapi.yaml`.

For an HTTP listener configured with `auth: api_key`, downstream clients must send:

```text
Proxy-Authorization: Bearer psk_...
```

The key is verified against the stored hash and enabled client record. It is removed
before any origin request. SOCKS5 username/password auth has not yet been wired to
clients, so use the documented loopback local mode for SOCKS during development.

## Audit Events

The SQLite audit trail records actor, action, target, request ID and timestamp for
setup, login, logout and proxy/client/key mutations. It intentionally excludes raw
passwords, setup tokens, cookies, API keys and request/response bodies. `/api/v1/audit`
is administrator-only; retention/export and a dashboard audit page are pending.
