# ProxySieve milestone tracker

Maintainer: **Tony Nguyen**. Updated: 2026-09-11.

## Status

- [ ] M0 — Repository bootstrap (local checks passed; hosted acceptance pending)
- [ ] M1 — Domain, config, storage, secret foundations (implemented locally; acceptance review pending)
- [ ] M2 — Proxy normalization, sources, endpoint management (parser/import preview and safe HTTP source fetch/preview locally implemented; scheduler/source CRUD/API pending)
- [ ] M3 — HTTP forward and CONNECT gateway (local HTTP/CONNECT routing supports explicit direct and configured HTTP/HTTPS/SOCKS upstream pools; auth/accounting/health pending)
- [ ] M4 — SOCKS5 downstream and multi-listener support (local no-auth SOCKS5 CONNECT and runtime multi-listener support implemented; password auth/metrics pending)
- [ ] M5 — Deterministic policies and routing actions (evaluator locally implemented; runtime/API simulator pending)
- [ ] M6 — Pools, selectors, sessions, chaining (built-in selectors locally implemented; pool/session services and chaining pending)
- [ ] M7 — Health, circuit breaker, safe retries (health/circuit routing and conservative retry eligibility locally implemented; active checks/backoff/transport retry pending)
- [ ] M8 — Traffic, cost, budgets, retention (exact HTTP application-stream counters, bounded events and atomic hard-budget reservation foundation locally implemented; tunnel accounting/rollups/durable enforcement pending)
- [ ] M9 — Cache and advanced visible-HTTP actions
- [ ] M10 — Browser integrations
- [ ] M11 — API, authentication, RBAC, audit (first-run admin auth, Argon2id, role hierarchy, protected local API and SQLite user migration locally implemented; resource CRUD/API keys/audit/SSE pending)
- [ ] M12 — Admin dashboard and first-run UX
- [ ] M13 — Shadow policies, events, alerts, extensions
- [ ] M14 — Optional HTTPS Inspect
- [ ] M15 — Backup, restore, import/export, operations
- [ ] M16 — Hardening, benchmarks, release candidate and v1

## M0 verification

Verified locally on Windows amd64, Go 1.27.1 / Node 24.19.0 / pnpm 11.19.0:

- [x] gofmt clean; `go vet ./...` passed.
- [x] `go test -count=1 -cover ./...` passed; CLI/buildinfo each 100% statement
  coverage (the process-exit-only main has no unit coverage).
- [x] `go test -race ./...` passed using the installed C toolchain.
- [x] golangci-lint 2.13.2 reported 0 issues.
- [x] Native build and `version --json` smoke test passed; Tony Nguyen is credited.
- [x] Static cross-compilation passed for Linux/macOS/Windows, amd64 and arm64.
  Cross-compilation is not a runtime test on those six platforms.
- [x] Frontend frozen lockfile install, lint, 2 unit tests, and Vite build passed.
- [x] `pnpm audit --audit-level=high` found no known vulnerabilities.
- [x] govulncheck 1.8.0 found no vulnerabilities in reachable Go code.
- [x] actionlint 1.7.12 validated all three workflow files.
- [ ] Hosted CI (including native Linux/macOS execution).
- [ ] Container build and non-root smoke test (Docker is unavailable locally;
  backend CI includes this check).
- [ ] Hosted repository protection/security settings confirmed.

Publication gate: the existing remote is public. No push was performed. The user's
general continuation request did not satisfy the publication approval gate, so no
further publication attempt will be made without specific authorization. Hosted
checks remain pending, not passed. Local development continues on an unpublished
branch under the user's instruction to keep working; M0 hosted acceptance is not
waived. See ADR 0009 for this delivery-only adjustment.

The original plan's intentional Markdown hard-break spaces are preserved. The
staged whitespace check applies to newly authored files without rewriting that
source document.

Vite child-process execution and golangci-lint's user cache required approved
out-of-sandbox runs; no safety checks were disabled to work around those restrictions.
Go is installed at C:\Program Files\Go\bin but was not in this terminal's PATH.
No global toolchain install or PATH mutation was performed.

Release tooling is a scaffold, not a tested/published release. The default container
prints version only. Web tooling builds metadata, not a dashboard. Gateway/config
parsing and database functionality intentionally remain unavailable at M0.

Repository initially had only the untracked original plan, no commits, and an empty
remote at https://github.com/nguyenduytan/proxysieve.git. No user code was replaced.
The maintainer restored `PROXYSIEVE_PLAN.md` to the workspace. It remains source
controlled until final release/review, when the maintainer may move it externally.

## Next milestone entry gate

Read PLAN.md and the original plan completely, inspect current code and tests,
verify M0 evidence, then start M1. Do not build gateway or dashboard before their
dependencies. Record refinements R1/R2/R5/R8 in the M1 domain/config tests.
