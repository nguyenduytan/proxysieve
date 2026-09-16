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
GET  /api/v1/traffic/breakdown
GET  /api/v1/budgets
GET  /api/v1/cache/stats
POST /api/v1/cache/purge
GET  /api/v1/health/proxies
GET  /api/v1/health/pools
POST /api/v1/health/proxies/{id}/check
POST /api/v1/health/pools/{id}/check
GET  /api/v1/audit
GET  /api/v1/proxies
POST /api/v1/proxies
POST /api/v1/proxies/import/preview
POST /api/v1/proxies/import
GET  /api/v1/proxies/{id}
PATCH /api/v1/proxies/{id}
DELETE /api/v1/proxies/{id}
GET  /api/v1/sources
POST /api/v1/sources
GET  /api/v1/sources/{id}
PATCH /api/v1/sources/{id}
DELETE /api/v1/sources/{id}
GET  /api/v1/pools
POST /api/v1/pools
GET  /api/v1/pools/{id}
PATCH /api/v1/pools/{id}
DELETE /api/v1/pools/{id}
GET  /api/v1/policies
POST /api/v1/policies
GET  /api/v1/policies/{id}
PATCH /api/v1/policies/{id}
DELETE /api/v1/policies/{id}
POST /api/v1/policies/{id}/simulate
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
Overview, Traffic, Budgets, Cache, Proxies, Sources, Pools, Chains, Health, Sessions,
Policies, Clients, Audit and System. Planned
workspaces such as Alerts are not rendered as disabled navigation. This avoids
duplicate or inert menu surfaces while features are still under development.

`GET /api/v1/budgets` exposes the active file-configured budgets and their current
durable usage to viewers. Calendar budgets include the current half-open UTC window;
lifetime budgets omit bounds. The response reports used, reserved and remaining
bytes plus exhaustion state. Budget mutation remains configuration-only until
persistent CRUD and runtime activation are implemented.

`GET /api/v1/cache/stats` exposes whether the in-memory response cache is enabled,
plus its bounded capacity, stored and served bytes, hit/miss/bypass counts,
expiration, eviction and hit ratio. Operators may clear all response entries with
the CSRF-protected `POST /api/v1/cache/purge`; the mutation is audited as
`cache.purged`. Purging does not reset cumulative process-lifetime counters.
The matching `proxysieve cache stats` and `proxysieve cache purge` commands use
the same Admin API and read the password from `PSV_ADMIN_PASSWORD`; command output
never includes the password or session cookies.

Health collection endpoints are viewer-readable. Manual checks require an
operator session and CSRF token, accept a validated host and port, and apply the
runtime private-destination policy before connecting. Proxy and pool checks share
the active runtime state; overlapping work for one proxy is rejected. Disabled
resources remain visible but are not checkable. Check traffic is recorded under
the dedicated `health_check_bytes` counter. Proxy rows expose the last 100
accepted outcome signals and success rate; these rolling values reset when the
process restarts.

Proxy inventory updates and deletes use an optimistic `revision` precondition. Send
the complete endpoint document with its current revision to `PATCH`, or the current
revision alone to `DELETE`; stale revisions return `409 PROXY_CONFLICT`. These
mutations remain inventory-only and require an operator session plus CSRF token.

`POST /api/v1/proxies/import` commits a parser-approved text import in one storage
transaction. Its optional `mode` is `skip` (default), `update`, or `create`; malformed
lines are ignored after validation and the response reports created, updated and
skipped counts. The import never persists credentials embedded in source text.

Proxy source records use the same optimistic revision discipline. Viewer sessions
may list and inspect sources; operators may create, replace or delete source
metadata with a valid CSRF token. `last_refresh_at` and `last_refresh_status` are
server-managed fields, so PATCH requests cannot forge refresh results. Source
`config` is bounded non-secret metadata; credentials belong in `credential_ref`.
`POST /api/v1/sources/{id}/refresh` fetches an enabled HTTP(S) API source through
the default-deny destination policy, parses its bounded response, and atomically
reconciles matching endpoint inventory with refresh status. A bounded scheduler
uses the same refresh coordinator for due sources. The Admin Sources workspace
exposes this lifecycle to viewers and operators without implying that saved
endpoints are automatically activated in runtime pools or policies.

Pool records use optimistic revisions and are readable by viewers; operators may
create, replace or delete them with CSRF protection. The API rejects missing proxy
members, missing fallback pools and fallback cycles. A pool used as a fallback,
or a proxy assigned to a saved pool, cannot be deleted until the reference is
removed. These reference-sensitive control-plane mutations are serialized within
the running server. The Pools workspace loads the complete paginated proxy and
pool inventory for its selectors and keeps list feedback separate from form
validation. Persistence remains inventory-only: no saved pool changes the running
gateway until an operator activates the complete inventory. See
[pool inventory](pools.md) for the boundary and remaining limitations.

Runtime sticky bindings are exposed through bounded `GET /api/v1/sessions` and
`GET /api/v1/sessions/{id}` responses. Viewers can inspect HMAC-indexed session
metadata; operators can rotate or delete a binding through CSRF-protected,
audited mutations. Raw affinity keys are never stored or returned. Rotation
changes only subsequent requests and does not interrupt an active tunnel. The
Sessions workspace provides the same role-aware surface; see
[sticky sessions](sessions.md).

Policy records use the same optimistic revision discipline. Viewers may list,
inspect and simulate policies; operators may create, replace or delete policy
metadata with a valid CSRF token. Policy validation bounds rule names, condition
trees and action values, compiles regex/wildcard/CIDR conditions, and verifies
that every proxy action references an existing pool. A pool referenced by a saved
policy cannot be deleted. Simulation calls the canonical evaluator and returns
condition traces without changing live routing. All policy responses explicitly
report whether the saved document is `active` or `staged`, plus the active runtime
revision; see
[policy inventory and simulation](policies.md).

Runtime state is exposed through `GET /api/v1/runtime` and bounded newest-first
`GET /api/v1/runtime/history`. Operators activate the complete staged inventory
with `POST /api/v1/runtime/activate` and can republish a retained revision through
`POST /api/v1/runtime/rollback`; both mutations require CSRF and an optimistic
expected runtime revision. Responses expose counts and whether staged inventory
differs, not the complete snapshot document. See
[runtime activation and rollback](runtime-activation.md).

## Client API Keys

An admin may create an enabled logical client with `POST /api/v1/clients`, then
create a downstream key with `POST /api/v1/clients/{id}/api-keys`. Client records
use optimistic revisions for `GET`, `PATCH` and `DELETE`; deleting a client also
deletes its issued keys. The raw key is returned only by its creation response.
SQLite retains a SHA-256 hash and display prefix, never the raw token.
Administrators can list clients and key metadata, revoke an existing key with the
CSRF-protected DELETE route, and use the embedded Clients workspace for these
operations. A machine-readable contract is available at `GET /api/v1/openapi.yaml`.

For an HTTP listener configured with `auth: api_key`, downstream clients must send:

```text
Proxy-Authorization: Bearer psk_...
```

The key is verified against the stored hash and enabled client record. It is removed
before any origin request. SOCKS5 username/password auth is handled at the listener
boundary and maps the configured credential to an opaque client identity; API-key
authentication remains HTTP-only.

## Audit Events

The SQLite audit trail records actor, action, target, request ID and timestamp for
setup, login, logout and proxy/source/pool/policy/runtime/session/client/key
mutations. It intentionally excludes raw
passwords, setup tokens, cookies, API keys and request/response bodies. `/api/v1/audit`
is administrator-only. The embedded administrator-only Audit workspace presents
the latest 100 entries as read-only metadata with manual refresh and stable
cursor-based loading for older entries. It does not expose secrets, request
bodies or mutation controls; API-side export remains pending. The CLI portable
config export/import path is documented separately and does not expose secrets.
