# Development

## Toolchain

Go 1.27.x; Node 24 LTS; pnpm 11.19.0. Install from official distributions.
The frontend pins TypeScript 5.9.3 because the selected TypeScript ESLint release
does not support TypeScript 7; do not blindly install every package's latest tag.
Dependency changes must update `web/pnpm-lock.yaml` and pass all frontend checks.

M1 adds pinned YAML and pure-Go SQLite dependencies with go.sum. CI uses the real
checksum file for dependency caching. Public packages remain standard-library-only;
an architecture test rejects implementation dependencies across that boundary.

## Commands

```sh
gofmt -w cmd internal pkg
go vet ./...
go test -count=1 ./...
go test -race ./...
go build -trimpath -o bin/ ./cmd/proxysieve
go run ./cmd/proxysieve version --json
go run ./cmd/proxysieve config validate --file config.example.yaml
go run ./cmd/proxysieve doctor --file config.example.yaml
pnpm --dir web install --frozen-lockfile
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web build
```

Go race testing requires a supported C toolchain/platform; run on Linux CI if the
Windows workstation lacks one. Do not report an unexecuted race test as passing.
Use golangci-lint 2.13.2 for `golangci-lint run`; CI pins that version. The Makefile
wraps these raw commands and does not install global tooling silently.

## Frontend milestone boundary

M0 uses Vite library mode to verify TypeScript build/test/lint plumbing. It has no
SPA, dev server, routes, screenshots, or fake gateway dashboard. M12 replaces the
library entry with the API-driven React SPA and adds browser interaction tests.
React dependencies are pinned now to establish the planned stack.

## Repository conventions

`cmd/` owns process exit; `internal/cli` is independently testable; `internal/buildinfo`
contains public-safe provenance. `pkg/` contains M1 domain/config/store contracts.
`internal/configload`, `internal/security`, and `internal/storage` contain adapters.
The endpoint memory and SQLite stores share a behavioral test suite covering CRUD,
transactions, revisions, isolation, cancellation and pagination. Add further SQL
migrations, API schema, protocol fixtures, integrations, and benchmark commands
with their milestones, never empty success scripts.

Tests should not read developer credentials or dial public/paid proxies. Separate
deterministic integration fixtures from opt-in live experiments. Proxy credential
examples belong in fake fixtures, not shell history or committed configs.

## Release scaffold

`.goreleaser.yml` describes static cross-platform CLI archives and checksums; it is
not a release. `go build` has no web embedding at M0. Docker's final image is scratch,
non-root, and deliberately has no network defaults. Root CA bundles and runtime
storage/permissions must be introduced and tested with outbound transports in M3.
Do not publish tags/images until their milestone and release gates pass.

## Hosted setup still needs verification

Run CI before making its check names required. Enable repository security features,
Discussions, PR/squash workflow, and branch protection as described by the original
plan. Do not assume adding YAML also changes GitHub repository settings.
