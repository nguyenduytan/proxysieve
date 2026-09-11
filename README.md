# ProxySieve

**Smart traffic control for paid proxies.**

Created and maintained by **Tony Nguyen** · [@nguyenduytan](https://github.com/nguyenduytan)

Stop paying for bytes you don't need. Route smarter, filter earlier, measure everything.

## Current status: local gateway and control-plane foundation

This repository is under active development and is **not production-ready**. It
now has local HTTP forward, HTTPS CONNECT, SOCKS5 CONNECT, SQLite-backed first-run
admin setup and a protected versioned control-plane foundation,
but its default policy fails closed until a route is configured. The CLI provides
help, build/version metadata, configuration validation and effective-config inspection.
but it is not production-ready. Dashboard/API integration, full RBAC resource APIs,
health wiring, budgets, cache, browser filtering and optional HTTPS inspection have
not shipped.

See [milestone progress](PROGRESS.md), [the implementation contract](PLAN.md), and
[engineering refinements](docs/plan-refinements.md) for planned work and verification.
There is no stable release or published container to install yet.

## What we are building

ProxySieve will sit between clients and paid HTTP/SOCKS proxies to apply policies,
select healthy pools, preserve sticky sessions, and account for traffic and costs.
Browsers, scripts, CLI tools, and services will use standard proxy interfaces.
Optional Playwright/Puppeteer adapters will filter resources before they reach the
paid proxy. Providers stay replaceable adapters, not hardcoded core dependencies.

HTTPS CONNECT is opaque: domain/port rules do not reveal encrypted paths, images,
or headers. Browser integrations and explicitly enabled HTTPS inspection are
separate planned capabilities. Measured upstream bytes and estimated avoided bytes
will always be reported separately. Savings depend on workload and policy.

## Build the foundation

Prerequisites: Go **1.27.x** (tested 1.27.1), Node.js **24 LTS**, pnpm **11.19.0**.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/ ./cmd/proxysieve
go run ./cmd/proxysieve version
go run ./cmd/proxysieve version --json
go run ./cmd/proxysieve config validate --file config.example.yaml
go run ./cmd/proxysieve config print-effective --file config.example.yaml
go run ./cmd/proxysieve start --file config.example.yaml
pnpm --dir web install --frozen-lockfile
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web build
```

The executable is `bin/proxysieve` on Unix and `bin/proxysieve.exe` on Windows.
On Windows use PowerShell; Make is optional. If Go is installed but missing from
PATH, add its `bin` directory to your terminal's PATH (normally
`C:\Program Files\Go\bin`), then open a new terminal.

The frontend currently builds a TypeScript library entry as a tooling smoke test,
not an application. React 19.3 and Vite 8.1 are pinned for the planned dashboard.
Go dependencies and their checksums are pinned in go.mod/go.sum. YAML decoding and
SQLite remain internal adapters; public domain contracts use the standard library.
See [configuration](docs/configuration.md) and [security foundation](docs/security-foundation.md).

On the first local start with admin enabled, ProxySieve prints a one-time setup
token to the terminal. Do not paste it in issues, shells with shared history, or
logs. The admin server exposes local `/health`, `/ready`, `/api/v1/auth/setup-status`
and protected `/api/v1` routes. Full dashboard serving is a later milestone.

### Container scaffold

```sh
docker build -t proxysieve:dev .
docker run --rm --read-only --cap-drop=ALL --security-opt=no-new-privileges proxysieve:dev version
```

The current non-root container runs the CLI only; no ports are exposed. Compose
also defaults to `version`. Container runtime listeners and embedded SPA assets
arrive in later milestones, not through a misleading placeholder server.

## Planned safety defaults

Loopback binds, no open proxy, no implicit DIRECT fallback, no HTTPS inspection,
redacted secrets, private-destination restrictions for untrusted clients, and
bounded resource use. Until those capabilities are implemented and tested, do not
use this bootstrap with production traffic or credentials.

## Development and contribution

- [Contributing](CONTRIBUTING.md) and [development guide](docs/development.md)
- [Security reporting](SECURITY.md)
- [Support](SUPPORT.md) and [code of conduct](CODE_OF_CONDUCT.md)
- [Changelog](CHANGELOG.md)

## License and credits

Copyright 2026 **Tony Nguyen**. Licensed under [Apache-2.0](LICENSE).
See [NOTICE](NOTICE). Contributions remain credited; provider brands do not define
ProxySieve's identity. Branding is never injected into proxied user traffic.
