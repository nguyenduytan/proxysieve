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
make fuzz
go test -count=1 ./internal/app ./internal/upstream ./internal/transport/httpforward ./internal/transport/socks5
go test -run '^$' -bench 'Benchmark(PolicyEvaluation|Selector|TrafficRecordFullBuffer|ResponseCacheHit)$' -benchmem ./pkg/policy ./pkg/routing ./internal/traffic ./internal/cache
go build -trimpath -o bin/ ./cmd/proxysieve
go run ./cmd/proxysieve version --json
go run ./cmd/proxysieve config validate --file config.example.yaml
go run ./cmd/proxysieve doctor --file config.example.yaml
go run ./cmd/proxysieve backup --file config.example.yaml --path ./proxysieve-backup.db
go run ./cmd/proxysieve restore --file config.example.yaml --path ./proxysieve-backup.db
pnpm --dir web install --frozen-lockfile
pnpm --dir web lint
pnpm --dir web test
pnpm --dir web build
```

Go race testing requires a supported C toolchain/platform; run on Linux CI if the
Windows workstation lacks one. Do not report an unexecuted race test as passing.
Use golangci-lint 2.13.2 for `golangci-lint run`; CI pins that version. The Makefile
wraps these raw commands and does not install global tooling silently.

`make integration` runs the local HTTP, CONNECT, SOCKS5 and upstream connector
matrix. Its path-gated workflow repeats the same network-only fixtures natively on
Linux, Windows and macOS; no public host or paid proxy is contacted.

`make fuzz` gives each trust-boundary target ten seconds. The same bounded smoke
run executes on relevant pull requests and weekly; longer local campaigns can pass
the raw `go test -fuzz` commands a larger `-fuzztime`.

`make benchmark` measures policy evaluation at 100/1,000/10,000 rules, selection at
100/1,000 endpoints, a full traffic recorder and the response-cache hit path. The
benchmark workflow retains five-run results as an artifact. Results are evidence,
not a release threshold, until the first accepted release-candidate baseline exists.

## Frontend workflow

The API-driven React SPA is built by Vite and embedded under `internal/api/ui/`.
Frontend changes must pass lint, unit tests and the production build; user-facing
workflow changes also require desktop/mobile browser QA against a local API fixture
or runtime. The dashboard uses same-origin `/api/v1` routes and does not read SQLite
directly.

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

`.goreleaser.yml` describes static cross-platform archives with the embedded Admin
SPA, sample configuration, operator docs and checksums. A semantic-version tag runs
the pinned release workflow, adds an SPDX SBOM and provenance, and creates a draft
release for smoke testing; see [release.md](release.md). Local SQLite backup/restore
uses WAL-consistent snapshots, schema validation, pre-restore archives and atomic
replacement; stop the process before either operation. Docker's final image is
scratch, non-root, includes the CA roots needed by outbound TLS and deliberately has
no listener defaults. Do not publish a draft or image until its release gates pass.

## Hosted setup still needs verification

Run CI before making its check names required. Enable repository security features,
Discussions, PR/squash workflow, and branch protection as described by the original
plan. Do not assume adding YAML also changes GitHub repository settings.
