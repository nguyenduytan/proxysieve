<div align="center">
  <h1>ProxySieve</h1>
  <p><strong>Smart traffic control for paid proxies.</strong></p>
  <p>Stop paying for bytes you do not need. Route smarter, filter earlier, measure everything.</p>
  <p>
    <a href="https://github.com/nguyenduytan/proxysieve/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/nguyenduytan/proxysieve/ci.yml?branch=main&label=backend&style=flat-square" alt="Backend CI" /></a>
    <a href="https://github.com/nguyenduytan/proxysieve/actions/workflows/frontend.yml"><img src="https://img.shields.io/github/actions/workflow/status/nguyenduytan/proxysieve/frontend.yml?branch=main&label=frontend&style=flat-square" alt="Frontend CI" /></a>
    <img src="https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white&style=flat-square" alt="Go 1.27.1" />
    <img src="https://img.shields.io/badge/React-19.3-149ECA?logo=react&logoColor=white&style=flat-square" alt="React 19.3" />
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-3DA639?style=flat-square" alt="Apache 2.0 license" /></a>
    <img src="https://img.shields.io/badge/status-development-F59E0B?style=flat-square" alt="Development status" />
  </p>
  <p>Created and maintained by <strong>Tony Nguyen</strong> · <a href="https://github.com/nguyenduytan">@nguyenduytan</a></p>
</div>

## Status

ProxySieve is an active **development build**, not a production release. It has a
locally tested HTTP forward/CONNECT and SOCKS5 CONNECT foundation, configurable
upstream HTTP/HTTPS/SOCKS routes, a protected local admin API, SQLite-backed
first-run setup, and an embedded operational dashboard.

The project intentionally fails closed. Saved endpoint, pool and policy edits are
staged until an operator explicitly activates the complete inventory as one
revision. There is no stable release, hosted dashboard, or
published container image yet.

## Architecture

```text
Client
  |
  +-- HTTP forward / CONNECT
  +-- SOCKS5 CONNECT
          |
          v
  ProxySieve listener
          |
          v
  Policy -> route decision -> pool selector -> health / budget guard
          |
          +-- BLOCK / REJECT
          +-- DIRECT (explicit allowlist only)
          +-- PROXY (HTTP / HTTPS / SOCKS upstream)
          |
          v
  Accounting + protected local control plane
```

Encrypted CONNECT and SOCKS tunnels expose only connection metadata such as target
host and port. ProxySieve does not claim path, header, MIME, or resource visibility
inside those tunnels. Browser integrations and optional HTTPS inspection are later,
explicitly scoped features.

## Current Capabilities

| Area | Available locally | Still in progress |
| --- | --- | --- |
| Gateway | HTTP forward, HTTPS CONNECT, SOCKS5 CONNECT, HTTP Basic/SOCKS5 password auth, graceful shutdown, listener limits | UDP, transport-framing accounting |
| Routing | Deterministic policies, atomic revisioned inventory activation/rollback, simulation, health/latency-aware selection, durable bounded sticky affinity, ordered HTTP/HTTPS/SOCKS proxy chains with active probes, configurable bounded safe retry, endpoint/pool/alternative-chain failover, configured upstream HTTP/HTTPS/SOCKS | Per-hop chain cost |
| Safety | Loopback defaults, private destination checks, pinned DIRECT DNS, no implicit DIRECT, source fetch guard | Persistent encrypted secret store, TLS remote admin |
| Operations | SQLite migrations, first-run admin setup, Argon2id password hashing, session-bound CSRF, RBAC, audited revisioned proxy/source/pool/policy/client/API-key APIs, cache stats/full/exact-host purge API and CLI, canonical policy simulation, atomic proxy-source refresh API/scheduler, WAL-consistent local backup/restore, versioned safe config import/export and published OpenAPI contract | SSE |
| Measurement | HTTP/CONNECT/SOCKS5 application-stream counters, policy/rule attribution, newest-event live buffer, bounded batched SQLite history, restart-safe minute/hour/day rollups, tier-specific pruning, bounded summary/timeseries/breakdown queries, exact cache savings, evidence-separated avoided-byte estimates, 30-day paid-traffic/configured-cost projection, currency-separated configured-cost estimates, rolling health/timing/throughput and circuit state, restart-safe lifetime/daily/weekly/monthly hard byte-budget enforcement | Transport/proxy framing, rolling windows, soft/cost budgets, provider billing reconciliation |
| Cache | Safe opt-in bounded memory response cache with client/session partitioning, explicit HTTP freshness/size eligibility, bounded TTL-based DNS cache, expired-first deterministic eviction, complete response-cache statistics and audited full/exact-host operator purge | Disk cache, full CACHE action |
| Dashboard | Embedded authenticated admin shell with Overview, Traffic, Budgets, Cache, Proxies, Sources, Pools, Chains, Health, Sessions, Policies, Clients, Audit and System; responsive navigation; inventory CRUD/import/simulation/activation/rollback workflows; health checks; cache operations; chain probes; session inspection/rotation; source scheduling; client/API-key lifecycle; sanitized audit history; real 24-hour traffic metrics/chart | Full visual rule builder, broader analytics/cost views, Alerts when its backend exists |

Read [PROGRESS.md](PROGRESS.md) for the precise milestone checklist. A green local
test does not imply an unimplemented capability is available.

## Quick Start

Prerequisites: Go **1.27.x** (tested 1.27.1), Node.js **24 LTS**, and pnpm
**11.19.0** for dashboard development.

```sh
go test ./...
go build -trimpath -o bin/ ./cmd/proxysieve
./bin/proxysieve start --file config.example.yaml
./bin/proxysieve doctor --file config.example.yaml
./bin/proxysieve backup --file config.example.yaml --path ./proxysieve-backup.db
./bin/proxysieve restore --file config.example.yaml --path ./proxysieve-backup.db
PSV_ADMIN_PASSWORD=... ./bin/proxysieve cache stats --username admin
PSV_ADMIN_PASSWORD=... ./bin/proxysieve cache purge --username admin
PSV_ADMIN_PASSWORD=... ./bin/proxysieve cache purge-domain --domain static.example.com --username admin
```

On Windows, run `bin\proxysieve.exe start --file config.example.yaml` from
PowerShell. If Go is installed but not on `PATH`, use its normal installation
directory, typically `C:\Program Files\Go\bin`.

The admin control plane binds to `http://127.0.0.1:9090` by default. On first run,
the terminal prints a one-time setup token that expires after 10 minutes. Use it
only in the local setup form. It is never returned by the API.

The example configuration is intentionally fail-closed. It starts the local
listener/control plane but does not route user traffic until a policy/pool/endpoint
configuration explicitly permits it. See [configuration](docs/configuration.md)
and [admin API foundation](docs/admin-api-foundation.md).
Operational backup and portable configuration workflows are described in
[operations](docs/operations.md).

`proxysieve doctor` validates the effective configuration and reports data-directory,
SQLite schema and listener-bind readiness without starting the gateway. Add `--json`
for automation; a failed critical check returns a non-zero exit code.

Stop the running ProxySieve process before running `backup` or `restore`. Backup
creates a consistent SQLite snapshot, including WAL state, and never overwrites an
existing destination. Restore validates the backup schema, archives the current
database and any WAL/SHM sidecars as a timestamped pre-restore copy, then installs
the backup atomically. Portable config export/import is available through
`proxysieve export` and `proxysieve import`; it excludes raw secrets, validates the
manifest/config before writing, refuses overwrite and supports `--dry-run`.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run

pnpm --dir web install --frozen-lockfile
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web build
pnpm --dir web dev
```

`pnpm --dir web build` produces immutable SPA assets under `internal/api/ui/` for
embedding in the Go binary. The dashboard consumes same-origin `/api/v1` routes;
it never opens SQLite directly.

## Security Model

- Listener and admin defaults bind to loopback only.
- DIRECT is disabled unless both global configuration and a policy allowlist permit it.
- Private, loopback, link-local, multicast and unspecified destinations are denied
  for untrusted traffic.
- Proxy credentials use secret references, not YAML URL userinfo or endpoint metadata.
- Sensitive headers, URL userinfo and query values are redacted in diagnostic helpers.
- Admin setup is single-use; passwords use Argon2id; mutation APIs need a
  session-bound CSRF token and same-origin request validation.
- Upstream remote DNS is an explicit trust boundary. Strict private-destination
  protection requires `trusted_remote_dns: true` on an operator-approved endpoint.

Please read [SECURITY.md](SECURITY.md) before reporting a vulnerability. Do not
paste proxy credentials, cookies, API keys, setup tokens, or user traffic in issues.

## Documentation

- [Implementation plan](PROXYSIEVE_PLAN.md)
- [Milestone progress](PROGRESS.md)
- [Engineering refinements](docs/plan-refinements.md)
- [Configuration](docs/configuration.md)
- [Admin control plane](docs/admin-api-foundation.md)
- [Pool inventory](docs/pools.md)
- [Proxy chains](docs/chains.md)
- [Sticky sessions](docs/sessions.md)
- [Policy inventory and simulation](docs/policies.md)
- [Runtime activation and rollback](docs/runtime-activation.md)
- [Proxy source security](docs/proxy-sources.md)
- [Traffic accounting](docs/traffic-accounting.md)
- [Development guide](docs/development.md)
- [Release process and artifact verification](docs/release.md)
- [Contributing](CONTRIBUTING.md)

## License And Credits

Copyright 2026 **Tony Nguyen**. ProxySieve is licensed under
[Apache-2.0](LICENSE); attribution details are in [NOTICE](NOTICE). Contributions
remain credited. Provider brands are adapters, never part of the core identity, and
ProxySieve never injects branding into proxied traffic.
