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

The project intentionally fails closed. A configured endpoint inventory does not
activate a live route by itself; explicit policy, pool, credential and runtime
configuration are required. There is no stable release, hosted dashboard, or
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
| Gateway | HTTP forward, HTTPS CONNECT, SOCKS5 CONNECT, graceful shutdown, listener limits | SOCKS password auth, UDP, transport-framing accounting |
| Routing | Deterministic policies, pools, selection strategies, configured upstream HTTP/HTTPS/SOCKS | Persisted pool management, chains, full failover |
| Safety | Loopback defaults, private destination checks, pinned DIRECT DNS, no implicit DIRECT, source fetch guard | Persistent encrypted secret store, TLS remote admin |
| Operations | SQLite migrations, first-run admin setup, Argon2id password hashing, session-bound CSRF, RBAC, audited proxy-source and client/API-key lifecycle APIs, published OpenAPI contract | Source refresh scheduling/UI, API-key list/revocation UI, SSE, backup/restore |
| Measurement | HTTP/CONNECT/SOCKS5 application-stream counters, newest-event live buffer, bounded batched SQLite history, restart-safe minute/hour/day rollups, tier-specific pruning, bounded summary/timeseries queries, currency-separated configured-cost estimates, health/circuit state, restart-safe hard byte-budget enforcement | Transport/proxy framing, billing windows, soft/cost budgets, projections, provider billing reconciliation |
| Dashboard | Embedded authenticated admin shell with Overview, Traffic, Proxies and System; responsive navigation; proxy inventory/import preview; real 24-hour traffic metrics/chart | Full CRUD for every domain, broader analytics/cost views, Alerts when its backend exists |

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

`proxysieve doctor` validates the effective configuration and reports data-directory,
SQLite schema and listener-bind readiness without starting the gateway. Add `--json`
for automation; a failed critical check returns a non-zero exit code.

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
- [Proxy source security](docs/proxy-sources.md)
- [Traffic accounting](docs/traffic-accounting.md)
- [Development guide](docs/development.md)
- [Contributing](CONTRIBUTING.md)

## License And Credits

Copyright 2026 **Tony Nguyen**. ProxySieve is licensed under
[Apache-2.0](LICENSE); attribution details are in [NOTICE](NOTICE). Contributions
remain credited. Provider brands are adapters, never part of the core identity, and
ProxySieve never injects branding into proxied traffic.
