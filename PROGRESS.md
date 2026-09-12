# ProxySieve milestone tracker

Maintainer: **Tony Nguyen**. Updated: 2026-09-12.

## Status

- [ ] M0 — Repository bootstrap (local checks passed; hosted acceptance pending)
- [ ] M1 — Domain, config, storage, secret foundations (implemented locally; acceptance review pending)
- [ ] M2 — Proxy normalization, sources, endpoint management (parser/import, safe HTTP source fetch/preview, revisioned source CRUD, responsive source UI and atomic manual/automatic refresh locally implemented; broader formats pending)
- [ ] M3 — HTTP forward and CONNECT gateway (local HTTP/CONNECT routing supports explicit direct and configured HTTP/HTTPS/SOCKS upstream pools; auth/accounting/health pending)
- [ ] M4 — SOCKS5 downstream and multi-listener support (local no-auth SOCKS5 CONNECT and runtime multi-listener support implemented; password auth/metrics pending)
- [ ] M5 — Deterministic policies and routing actions (evaluator locally implemented; runtime/API simulator pending)
- [ ] M6 — Pools, selectors, sessions, chaining (built-in selectors locally implemented; pool/session services and chaining pending)
- [ ] M7 — Health, circuit breaker, safe retries (health/circuit routing and conservative retry eligibility locally implemented; active checks/backoff/transport retry pending)
- [ ] M8 — Traffic, cost, budgets, retention (HTTP/CONNECT/SOCKS5 application-stream counters, bounded live/SQLite queues, batched history, restart-safe minute/hour/day rollups, bounded summary/timeseries API, independent four-tier retention, currency-separated configured-cost snapshots/analytics and restart-safe hard byte-budget enforcement locally implemented; transport framing, billing windows, projections, soft thresholds and cost budgets pending)
- [ ] M9 — Cache and advanced visible-HTTP actions
- [ ] M10 — Browser integrations
- [ ] M11 — API, authentication, RBAC, audit (first-run admin auth, Argon2id, role hierarchy, protected local API, SQLite user migration, audited client/API-key lifecycle endpoints and embedded OpenAPI contract locally implemented; broader resource APIs and SSE pending)
- [ ] M12 — Admin dashboard and first-run UX (authenticated responsive shell, Overview/Traffic/Proxies/Sources/System and first-run setup locally implemented; remaining resource workflows and release UX pending)
- [ ] M13 — Shadow policies, events, alerts, extensions
- [ ] M14 — Optional HTTPS Inspect
- [ ] M15 — Backup, restore, import/export, operations (doctor command now checks effective config, data directory, SQLite schema and listener availability; backup/restore/import/export still pending)
- [ ] M16 — Hardening, benchmarks, release candidate and v1

### Latest local continuation — proxy inventory lifecycle (2026-09-12)

- Added authenticated `GET`, optimistic-revision `PATCH` and `DELETE` endpoints
  for `/api/v1/proxies/{id}` with operator CSRF protection, audit events and
  stale-write conflict responses.
- Added atomic `POST /api/v1/proxies/import` commits with bounded parsing and
  `skip`/`update`/`create` duplicate modes; embedded credentials remain stripped.
- Connected the Admin Proxies workspace to atomic import commits and
  optimistic-revision enable/disable controls, including viewer-only behavior and
  conflict refresh feedback. Mobile inventory rows now use action-visible cards.
- Added bounded proxy-source configuration, matching in-memory/SQLite revisioned
  repositories and audited operator CRUD endpoints. Refresh state remains
  server-managed and cannot be forged through source PATCH requests.
- Added operator-only, CSRF-protected API-source refresh with default-deny private
  destination policy, optimistic revisions, duplicate-safe reconciliation and a
  shared SQLite transaction for endpoints plus refresh status. Fetch/parse errors
  preserve the prior inventory and persist only bounded safe status text.
- Added a bounded rotating source scheduler with per-source timeouts, due-time
  checks, safe continuation after individual failures and scheduled audit events.
  Manual and scheduled refreshes share one per-source overlap coordinator.
- Added the responsive Admin Sources workspace with viewer-safe listing and
  operator create/edit, enable/disable, manual refresh, delete confirmation,
  optimistic-conflict recovery and mobile cards. Pools, policies and runtime
  activation remain pending.
- Updated the embedded OpenAPI contract and API foundation documentation.

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

### Latest local continuation — traffic maintenance (2026-09-11)

Continued the existing uncommitted traffic-history/scheduler implementation; no
public push or release was performed. Preserved the existing API/frontend edits.

- Added migration 6 with a monotonic retention watermark and dirty-minute index;
  migration 5 remains unchanged by this continuation.
- Retention now aggregates before deletion in one transaction and preserves
  complete bucket totals after restart. Partial windows are rejected; partial
  cutoff minutes are retained. SUM overflow leaves raw data and prior totals intact.
- Scheduler catches up after downtime, rebuilds only dirty buckets, respects the
  configured interval, and joins its worker before database shutdown.
- Passed: `go test -count=1 ./...`, `go vet ./...`, and
  `go test -race ./internal/scheduler ./internal/storage/sqlite ./internal/app ./pkg/traffic`.
- Added tests for schema-5 upgrade, persisted history/retention across reopen,
  late events, backward cutoffs, invalid time ranges, overflow rollback and
  concurrent scheduler lifecycle.
- Frontend/browser QA, load testing, hosted CI and Docker were not rerun for this
  backend-only continuation. M8 acceptance remains open; see
  [traffic accounting](docs/traffic-accounting.md) for remaining limitations.

### Admin navigation and asynchronous traffic continuation — 2026-09-11

- Removed the legacy prototype component/data/type tree and its second Alerts
  surface. The primary sidebar now has one typed source of truth and contains only
  working API-backed destinations: Overview, Traffic, Proxies and System. Roadmap
  items no longer masquerade as disabled product navigation.
- Renamed Live Traffic to Traffic because the page intentionally merges live and
  retained events. Fixed merge identity/order so reused request IDs on different
  connections remain distinct and newest rows are stable.
- Added a bounded asynchronous persistence writer with atomic SQLite batches,
  queue/write-loss statistics, shutdown drain and newest-event live-buffer
  eviction. Visible HTTP policy rejections now produce zero-byte traffic events.
- Browser QA passed at the default desktop viewport and 390×844: login, all four
  destinations, pause/resume, proxy create/preview toggles, System, dark mode,
  mobile open/close/auto-close, page titles and console health. No Alerts label or
  inert navigation remained. No fake operational data was used.
- Passed 5 frontend test files / 14 tests, TypeScript, ESLint, Prettier and Vite
  production build. Passed `go test -count=1 ./...`, `go vet ./...`, and focused
  race tests across traffic, SQLite, API, app, scheduler and HTTP forwarding.
- No push or release was performed. Load/long-duration queue testing, other browser
  engines, hosted CI and Docker remain pending.

### Hierarchical traffic analytics continuation — 2026-09-11

- Added schema 7 hour/day aggregates with transactional dirty propagation from
  minute to hour to day. Late arrivals rebuild only affected complete buckets;
  startup catch-up now runs every tier before the next scheduler interval.
- Added authenticated, bounded `traffic/summary` and `traffic/timeseries` APIs.
  Queries use half-open aligned UTC ranges, optional client/pool/proxy/action/
  protocol filters, and cap series output at 2,000 buckets.
- Analytics combine retained minute aggregates with non-retired raw events without
  double-counting. Results remain stable when raw retention advances.
- Overview now uses the real 24-hour summary and an hourly request chart. The
  visible retained `REJECT` event, zero persistence loss and empty console were
  verified in the in-app browser after a process restart.
- Passed the full Go test suite and vet, focused Go race tests, 6 frontend test
  files / 16 tests, ESLint, TypeScript, Prettier and the Vite production build.
  No push or release was performed.

### Tiered traffic retention continuation — 2026-09-12

- Added schema 8 independent raw/minute/hour/day retention watermarks and one
  transactional compaction path that preserves canonical totals across restart.
- Added configurable minute/hour/day retention durations while preserving the
  legacy raw `retention_days` field; scheduler normalization prevents a coarser
  tier from becoming shorter than its source tier.

### Tunnel traffic continuation — 2026-09-12

- Added application-stream accounting for successful HTTP CONNECT and SOCKS5
  CONNECT tunnels, including separate downstream and selected-route counters.
- Paid proxy tunnel bytes populate upstream counters; direct tunnel bytes remain
  separate. Policy rejection and dial-failure attempts emit zero-byte events.
- Updated the Admin Traffic view to identify protocol and protocol-specific status,
  and removed obsolete HTTP-only/tunnel-unavailable wording.

### Configured-cost continuation — 2026-09-12

- Added schema 9 rate snapshots plus currency-separated minute/hour/day cost
  aggregates. Summary/timeseries APIs now expose configured estimated cost and the
  upstream application-stream bytes covered by each currency/rate.
- HTTP, CONNECT and SOCKS5 paid routes calculate cost from the selected endpoint's
  active rate; direct/cache/unrated traffic is never presented as zero-cost rated
  traffic. Cost survives tier retention and restart without mixing currencies.
- Renamed Admin traffic count labels from requests to events because tunnel
  connections are traffic events, not HTTP requests.

### Durable hard-budget continuation — 2026-09-12

- Added schema 10 durable budget usage/reservations and system/client/pool/proxy
  configuration scopes. Concurrent multi-scope reservations are atomic in SQLite.
- Paid HTTP, CONNECT and SOCKS5 streams reserve bounded chunks before upstream I/O,
  consume only completed bytes and return unused allowance. Admission rejects an
  exhausted budget before dialing the selected route.
- Startup conservatively converts crash-left reservations to used bytes, so restart
  cannot reset enforcement. Tests cover concurrency, multiple scopes, restart
  recovery, exact stream cutoff and runtime HTTP rejection.

### Original milestone entry gate

Read PLAN.md and the original plan completely, inspect current code and tests,
verify M0 evidence, then start M1. Do not build gateway or dashboard before their
dependencies. Record refinements R1/R2/R5/R8 in the M1 domain/config tests.
