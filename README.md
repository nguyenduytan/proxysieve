# ProxySieve

**Smart traffic control for paid proxies.**

Created and maintained by **Tony Nguyen** · [@nguyenduytan](https://github.com/nguyenduytan)

Stop paying for bytes you don't need. Route smarter, filter earlier, measure everything.

## Current status: M0 bootstrap

This repository is under active development, **not a usable proxy gateway yet**.
The CLI provides help and build/version metadata. `start` fails explicitly and
opens no listeners. No dashboard, routing, filtering, or savings metrics are shipped.

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

## Build the bootstrap

Prerequisites: Go **1.27.x** (tested 1.27.1), Node.js **24 LTS**, pnpm **11.19.0**.

```sh
go test ./...
go vet ./...
go build -trimpath -o bin/ ./cmd/proxysieve
go run ./cmd/proxysieve version
go run ./cmd/proxysieve version --json
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
The Go bootstrap uses only the standard library, so `go.sum` is not generated yet.

### Container scaffold

```sh
docker build -t proxysieve:dev .
docker run --rm --read-only --cap-drop=ALL --security-opt=no-new-privileges proxysieve:dev version
```

The current non-root container runs the CLI only; no ports are exposed. Compose
also defaults to `version`. Runtime listeners and embedded SPA assets arrive in
later milestones, not through a misleading placeholder server.

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
