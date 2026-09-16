# ProxySieve milestone tracker

Maintainer: **Tony Nguyen**. Updated: 2026-09-16.

## Status

- [ ] M0 — Repository bootstrap (local checks passed; hosted acceptance pending)
- [ ] M1 — Domain, config, storage, secret foundations (implemented locally; acceptance review pending)
- [ ] M2 — Proxy normalization, sources, endpoint management (parser/import, safe HTTP source fetch/preview, revisioned source CRUD, responsive source UI and atomic manual/automatic refresh locally implemented; broader formats pending)
- [ ] M3 — HTTP forward and CONNECT gateway (local HTTP/CONNECT routing supports explicit direct and configured HTTP/HTTPS/SOCKS upstream pools; auth/accounting/health pending)
- [ ] M4 — SOCKS5 downstream and multi-listener support (local/password SOCKS5 CONNECT and runtime multi-listener support implemented; UDP, metrics and transport-framing accounting pending)
- [ ] M5 — Deterministic policies and routing actions (evaluator, revisioned policy inventory, API simulation and durable atomic runtime activation/rollback locally implemented; full action execution pending)
- [x] M6 — Pools, selectors, sessions, chaining (built-in selectors, revisioned pool/chain inventory, runtime activation, bounded durable sticky affinity, ordered mandatory proxy chaining, active chain probes, audited session API/Admin/CLI observability and failover validation complete locally)
- [x] M7 — Health, circuit breaker, safe retries (passive/active checks, full rolling health/timing/throughput signals, score/latency selection, controlled half-open probes, globally/per-pool paced checks, health API/dashboard, configurable retry policy, per-attempt attribution and alternative-chain failover complete locally)
- [ ] M8 — Traffic, cost, budgets, retention (HTTP/CONNECT/SOCKS5 application-stream counters, policy/rule attribution, bounded live/SQLite queues, batched history, restart-safe minute/hour/day rollups, bounded summary/timeseries/breakdown API, independent four-tier retention, exact-vs-estimated savings display, 30-day paid-traffic/configured-cost projection, currency-separated configured-cost snapshots/analytics and restart-safe lifetime/calendar/rolling hard byte-budget enforcement with durable revisioned API/Admin CRUD locally implemented; transport framing, soft thresholds and cost budgets pending)
- [x] M9 — Cache and advanced visible-HTTP actions (policy-opt-in safe bounded memory/disk response cache, explicit HTTP freshness, deterministic TTL eviction, bounded DNS cache, complete process-lifetime cache statistics, audited full/exact-host purge API/CLI, role-aware Admin workspace and validated CACHE/THROTTLE/MOCK/REDIRECT/REWRITE execution complete locally)
- [x] M10 — Browser integrations (client-scoped snapshot/control contract, safe presets, Playwright/Puppeteer adapters, bounded estimated block reporting, Selenium foundation and controlled Chrome E2E complete locally)
- [ ] M11 — API, authentication, RBAC, audit (first-run admin auth, Argon2id, role hierarchy, protected local API, SQLite user migration, audited revisioned proxy/source/pool/policy/client/API-key/cache lifecycle endpoints, bounded authenticated traffic SSE and embedded OpenAPI contract locally implemented; broader API/event completion pending)
- [ ] M12 — Admin dashboard and first-run UX (authenticated responsive shell, API-backed operational workspaces including revisioned budget management and response-cache operations, first-run setup and policy runtime activation/rollback locally implemented; remaining release UX pending)
- [ ] M13 — Shadow policies, events, alerts, extensions
- [ ] M14 — Optional HTTPS Inspect
- [ ] M15 — Backup, restore, import/export, operations (doctor command checks effective config, data directory, SQLite schema and listener availability; safe local SQLite backup/restore and versioned secret-free config import/export are implemented)
- [ ] M16 — Hardening, benchmarks, release candidate and v1

### Release engineering continuation — 2026-09-16

- Completed M9 visible-HTTP actions. CACHE now explicitly opts a routed request
  into the configured safe cache backend; THROTTLE paces upload/download bytes;
  REWRITE is same-origin path/query only; MOCK and REDIRECT synthesize bounded
  responses. CONNECT/SOCKS fail closed and traffic attribution remains explicit.
- Added a SemVer-tag-gated, least-privilege release workflow using pinned actions
  and tool versions. GoReleaser produces draft Linux/Windows/macOS amd64/arm64
  archives and checksums; Syft adds an SPDX JSON SBOM and GitHub attests both the
  archive checksums and SBOM provenance.
- Added operator verification/publish guidance. Container publication, the hosted
  cross-platform run, benchmark/protocol/browser matrices and an actual RC remain
  pending, so M16 and the v1 release gate stay open.
- Fixed outbound TLS in the scratch container by copying a current CA bundle from
  the pinned Alpine build stage. The compose target remains an artifact smoke test,
  not an implicitly exposed gateway deployment.
- Fixed oversized cache-candidate streaming so the buffered prefix is replayed to
  the client without replacing the budgeted upstream reader. Regression coverage
  verifies exact delivery/accounting at the hard byte limit.
- Unavailable advanced policy actions now fail closed with explicit HTTP status and
  preserve their actual action in HTTP/CONNECT/SOCKS5 traffic attribution instead
  of appearing as destination-policy rejection.
- Added an authenticated bounded traffic SSE stream. Slow viewers are disconnected
  instead of blocking recorder hot paths; the Admin merges streamed events with
  the existing five-second polling fallback without duplicate rows.
- Added reproducible policy, selector, full-recorder and response-cache hit
  benchmarks plus a path-gated workflow that retains five-run results. Thresholds
  remain unset until an accepted release-candidate baseline exists.
- Completed the required nine-route HTTP/CONNECT/SOCKS5 protocol matrix by adding
  HTTP-through-SOCKS and proxied-tunnel coverage. A path-gated workflow runs the
  local-only fixtures natively on Linux, Windows and macOS.
- Added scheduled/path-gated fuzz smoke coverage at six trust boundaries. Proxy URI
  and CONNECT target lengths are now rejected before parser allocation; archive and
  future extension protocol fuzzing remain pending with those features.
- Replaced fixed durable traffic queue settings with validated schema-v1 capacity,
  batch and flush controls. A concurrent 8,000-event burst proves accepted events
  drain without analytics loss; long-duration and hosted load evidence remains.
- Completed the browser integration slice with API-key-authenticated, client-scoped
  active policy snapshots and bounded local-block reports recorded only as estimated
  avoided bytes. The shared evaluator fails open on expired/offline/unsupported or
  ambiguous policy state; integration metadata is never attached to origin traffic.
- Added runtime-dependency-free Playwright and Puppeteer adapters with safe presets,
  idempotent cleanup, cooperative interception behavior and Selenium proxy-only
  guidance. Unit tests cover forged attribution, policy scope, snapshot expiry,
  concurrent sessions and existing handlers. Controlled stable-Chrome E2E proves
  image/media/font/tracker requests never reach the local upstream proxy fixture
  while the document and XHR succeed. The hosted browser workflow remains pending
  until these commits are pushed.

### Latest local continuation — inventory lifecycle (2026-09-13)

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
  optimistic-conflict recovery and mobile cards. Source changes stage endpoint
  inventory for the explicit runtime activation workflow.
- Completed client/API-key lifecycle management: revisioned client GET/PATCH/
  DELETE with key cascade, admin-only Clients workspace, one-time key reveal,
  copy/dismiss handling, key listing/revocation and responsive client cards.
- Added the admin-only read-only Audit workspace with sanitized metadata,
  refresh/error/empty states and responsive mobile cards. Alerts remains absent
  from navigation until a real alert backend exists.
- Added stable timestamp/id cursor pagination to the Audit API and a responsive
  Load more control that appends older entries without exposing secrets.
- Added revisioned pool persistence in memory and SQLite, audited viewer/operator
  API routes, endpoint/fallback reference validation, fallback-cycle protection
  and optimistic conflict handling. Pool and proxy deletion now preserve saved
  references across serialized control-plane mutations.
- Added the responsive Pools workspace with parallel complete inventory loading,
  create/edit/enable-disable/delete controls, inline delete confirmation and one
  context-appropriate feedback surface. Saved pools remain staged until complete
  inventory activation succeeds.
- Added revisioned policy persistence in memory and SQLite, audited RBAC/CSRF-
  protected policy CRUD, strict condition/action validation, pool-reference-safe
  deletion, and a canonical-evaluator-backed simulation endpoint. The Policies
  workspace supports JSON authoring, optimistic revision updates, simulation and
  inline-confirmed deletion; runtime changes remain explicit rather than following
  every Save action.
- Updated the embedded OpenAPI contract and API foundation documentation.
- Added fail-closed SQLite backup/restore operations: WAL-consistent snapshots,
  schema validation, refusal to overwrite, WAL/SHM sidecar handling, timestamped
  pre-restore archives and atomic installation. The process must be stopped before
  either operation; portable config export/import validates a bounded manifest and
  writes only new files without raw secret material.
- Added durable runtime snapshots and operator activation/rollback APIs. Proxies,
  pools and policies are read in one SQLite transaction, fully validated and
  published as one immutable revision; stale activation is rejected and failures
  preserve last-known-good routing. Each request routes against its evaluation
  revision, and the active snapshot survives restart.
- Added an Admin Policies runtime strip with active revision/resource counts,
  explicit activation confirmation and retained-revision rollback. Viewer access
  remains read-only and action feedback stays in one context-appropriate surface.
- Runtime status now distinguishes synchronized and staged inventory after every
  activation or rollback, item responses identify exact active revisions, and
  both live-routing mutations require explicit confirmation. Activation is
  disabled when the saved inventory already matches the active snapshot.
- Fixed new-policy authoring so the required listener policy ID is preserved,
  including the default starter policy. Updated proxy/source/pool copy to use the
  same staged/active language throughout the Admin Panel.
- Verified the complete Admin flow in system Chrome at 1440x1000 and 390x844:
  first-run setup, proxy/pool/default-policy creation, revisions 1 and 2 activation,
  rollback to revision 1 as new revision 3, restart persistence, responsive
  navigation, no Alerts entry, no overflow/action overlap and no application
  console/page errors. The unauthenticated `/auth/me` session probe produces the
  expected 401 before login.
- Passed the full Go suite, vet and race detector; 12 frontend files / 28 tests,
  ESLint, TypeScript, Prettier and production asset build; binary `doctor`,
  WAL-consistent backup and atomic restore against SQLite schema 15.

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

Historical M0 publication note: the existing remote is public, but hosted checks
and repository settings remain independent acceptance gates. A local green run
does not waive them; the current branch may be pushed only as reviewed development
work, not tagged as `v1.0.0`.

The original plan's intentional Markdown hard-break spaces are preserved. The
staged whitespace check applies to newly authored files without rewriting that
source document.

### Latest local continuation — downstream password authentication (2026-09-14)

- Enabled listener `auth: password` for HTTP Basic and SOCKS5 RFC1929. Runtime
  resolves the referenced environment credential per connection, compares in
  constant time, and derives an opaque listener-scoped client identity without
  persisting or logging usernames/passwords.
- Added protocol-level success/failure and client-identity tests for both
  transports. API-key authentication remains HTTP-only; SOCKS5 UDP is still
  unsupported.

### Latest local continuation — bounded sticky affinity (2026-09-14)

- Added revisioned pool `session_policy` with explicit, client, destination and
  client-plus-destination affinity strategies, TTL/idle/request/byte bounds and
  rotation on revision, policy, endpoint health or upstream failure changes.
- Session keys are HMAC-indexed and raw values never enter storage, logs or
  upstream headers. HTTP cache keys now partition by downstream client and
  session hash; usage is recorded after HTTP/cache/tunnel completion.
- Added Admin Pool controls with native bounds, conditional limit fields and
  single-surface feedback. Added session documentation, OpenAPI schema and
  persistence/runtime activation coverage.
- Verified explicit affinity end to end with two upstreams, header isolation,
  cache partitioning, overflow atomicity, SQLite/memory round trips and
  desktop/mobile Admin QA. Added viewer session inspection and audited operator
  rotation/deletion in the API and responsive Admin workspace. SQLite-backed
  sessions and their HMAC namespace now survive restart. The session CLI supports
  list/show/rotate/delete through the authenticated Admin API. At that checkpoint,
  M6 remained open because chaining had not yet been implemented.

### Latest local continuation — ordered proxy chaining (2026-09-14)

- Added revisioned 2-to-8-hop chains with deterministic ordering, per-hop
  timeouts, disjoint endpoint validation and no silent hop/DIRECT fallback.
- Added policy chain actions, immutable runtime activation/rollback, nested
  HTTP/HTTPS/SOCKS5 tunnels and end-to-end chain routing coverage.
- Added audited chain CRUD and active-revision test API, sanitized health/failure
  state and latency, plus a responsive Admin Chains workspace with ordered hop
  editing and single-surface feedback.
- Added `chain_id` traffic filtering and restart-safe raw/minute/hour/day rollups.
  Chain configured cost remains unpriced until traffic events can carry one rate
  snapshot per hop without mixing currencies or double-counting stream bytes.
- Verified chain editing, ordering, probing, single-surface failure feedback,
  traffic attribution/search and runtime counts in the Admin Panel at desktop and
  390x844 without overflow or console errors. Full Go tests/coverage/race/vet,
  golangci-lint, govulncheck, actionlint, frontend lint/tests/build, dependency
  audit and six-target cross-compilation passed locally; Docker remains available
  only through hosted CI on this machine.

### Latest local continuation — health-aware failover (2026-09-15)

- Serialized half-open probes so only one recovery attempt can use a quarantined
  endpoint after cooldown; disabled endpoints never transition to half-open.
- Replaced placeholder selector metrics with rolling passive score/latency data;
  pool health and latency constraints now affect active runtime selection while
  unknown endpoints remain eligible to collect their first observation.
- Added one-step failover to a different eligible endpoint or chain member for
  bodyless safe HTTP requests and CONNECT/SOCKS5 dials before any client response.
  Every replacement route passes health, budget and half-open checks, uses bounded
  jittered backoff, and never falls back to DIRECT.
- Fixed untracked `session_policy: none` routes carrying a temporary session ID,
  which had incorrectly blocked failover by attempting to rotate a nonexistent
  stored session. Body-bearing requests are not replayed by transport adapters.
- Passed the full Go suite and vet plus focused race tests for health, app routing,
  HTTP forwarding, SOCKS5 and retry policy. Active checks, health API/Admin views
  and separate retry-attempt traffic/cost attribution remain M7 work.

### Latest local continuation — active health and retry attribution (2026-09-15)

- Added optional active checks with configurable target, interval and per-proxy
  timeout. Checks are serialized per endpoint, remain available without the Admin
  Panel, honor runtime cancellation and account proxy handshake bytes separately.
- Added viewer-readable proxy/pool health APIs and CSRF-protected operator checks
  with bounded input, destination-policy enforcement and sanitized error mapping.
- Added the responsive Admin Health workspace with one manual target and one
  feedback surface. Disabled resources are not checkable; refresh preserves the
  last data while loading.
- HTTP, CONNECT and SOCKS5 retries now record each attempted route separately while
  retaining shared request/connection IDs. Failed pre-response tunnel dials do not
  inherit application-stream bytes from the successful replacement route.
- Added health config bounds and proved schema-v1 files without a `health` section
  retain safe defaults. The health scheduler no longer truncates multi-proxy scans
  at the traffic-maintenance job deadline.
- Added a bounded 100-observation window for success/failure, timeout, proxy-auth,
  403/407/429 and 5xx signals. Transport failures retain timeout causes, 407 always
  degrades proxy health, and the Admin/API expose the explainable components.
- Added configurable global and per-pool probe pacing shared by active, manual
  proxy and manual pool checks. The per-proxy network timeout now lives in their
  common execution path instead of inheriting the longer Admin request timeout.
- Completed the M7 rolling signal set with preserved DNS/TLS causes, connection
  latency, HTTP time to first byte and application-stream throughput. HTTP traces
  use the standard library; CONNECT and SOCKS5 reuse their measured dial attempt.
- Route completion now updates throughput for the selected proxy or every chain
  hop and records session usage even when the optional traffic recorder is absent.
  The Admin Health table keeps its existing columns and shows timing/throughput as
  compact secondary values.
- Added global schema-v1 retry controls for one to five total attempts and explicit
  bodyless idempotency-key opt-in. Existing schema-v1 files retain conservative
  defaults, and one attempt disables retry without bypassing traffic accounting.
- Added ordered `fallback_chain_ids` to chain policy actions. Runtime retries the
  primary chain with different eligible endpoints before trying alternatives;
  every referenced chain is validated and protected from deletion, and no path
  falls back to `DIRECT`.
- Proved alternative-chain failover end to end with separate failed/successful
  traffic events, plus config compatibility, reference and no-retry regression
  coverage. M6 and M7 acceptance are complete locally.

Vite child-process execution and golangci-lint's user cache required approved
out-of-sandbox runs; no safety checks were disabled to work around those restrictions.
Go is installed at C:\Program Files\Go\bin but was not in this terminal's PATH.
No global toolchain install or PATH mutation was performed.

The M0 release tooling began as a scaffold. Current development now embeds the
Admin Panel and includes local gateway/database functionality, but no v1 tag or
published release exists while later milestone acceptance remains open.

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

### Calendar hard-budget continuation — 2026-09-16

- Added schema 19 usage rows keyed by budget and UTC window start. Existing lifetime
  usage upgrades under the zero window without resetting enforcement.
- Added daily, Monday-based weekly and monthly windows with mandatory IANA timezone
  names. Local calendar boundaries follow DST while persisted timestamps stay UTC.
- Tests cover a 23-hour DST day, weekly/monthly boundaries, local-midnight rollover,
  restart persistence and schema-18 migration.

### Budget observability continuation — 2026-09-16

- Added viewer-readable active budget status with normalized scope/window, current
  calendar bounds, durable used/reserved bytes, saturating remaining allowance and
  exhaustion state.
- Added the responsive read-only Admin Budgets workspace with native usage progress,
  deterministic scope/window details, empty/error/loading states and manual refresh.
- At this checkpoint, budget CRUD, soft thresholds and actions beyond hard rejection
  remained pending; no Alerts surface or placeholder editor was added.

### Rolling hard-budget continuation — 2026-09-16

- Added elapsed-time rolling hard budgets from 60 seconds through 365 days without
  adding a schema migration. Existing usage rows store UTC minute buckets.
- Enforcement aggregates the active buckets transactionally and keeps the cutoff
  minute charged until the next minute boundary, conservatively expiring by less
  than one minute instead of allowing an early reset.
- Added revisioned API/OpenAPI/Admin authoring, dynamic status bounds and restart,
  concurrent reservation, validation and boundary regression coverage.

### Revisioned budget inventory continuation — 2026-09-16

- Added schema 20 revisioned budget inventory with atomic one-time YAML import. Once
  initialized, SQLite remains authoritative even after every budget is deleted.
- Added operator CRUD with CSRF, optimistic revisions, audit events and immediate
  enforcement for new reservations. Existing leases keep their captured config.
- Added role-aware Admin create/edit/delete flows with one feedback surface and
  conflict refresh. Client, pool and proxy deletion now rejects active budget refs.
- Rolling windows, soft threshold events and cost-denominated budgets remain open.

### Response-cache operations continuation — 2026-09-16

- Fixed replacement accounting in the shared memory cache and added bounded
  expired-first/earliest-expiry eviction, so entry and byte limits remain true
  under replacement and pressure.
- Added process-lifetime hit, miss, bypass, expiration, eviction and bytes-served
  counters, current bytes/entries, capacity and hit ratio. Purging entries keeps
  the cumulative counters intact.
- Added viewer-readable `GET /api/v1/cache/stats` and operator-only,
  CSRF-protected `POST /api/v1/cache/purge`, including `cache.purged` audit events,
  explicit disabled state and the matching OpenAPI contract.
- Added `proxysieve cache stats|purge` on the existing bounded Admin CLI client;
  loopback/HTTPS URL enforcement, redirect refusal, cookie login and CSRF behavior
  are shared with the session commands.
- Added a role-aware responsive Cache workspace with real loading/error/disabled
  states, manual refresh and inline purge confirmation. Desktop/mobile browser QA
  found no horizontal overflow or console warnings/errors.
- Replaced the implicit one-minute lifetime with explicit `s-maxage`, `max-age`
  (adjusted by `Age`) or `Expires` freshness. Requests and responses with
  `no-cache`, stale/invalid freshness, partial content or unsupported `Vary`
  semantics bypass storage.
- Added exact-hostname purge without subdomain matching through the
  operator/CSRF-protected API, `cache purge-domain` CLI command and responsive
  Cache workspace. The mutation has a dedicated `cache.domain_purged` audit event.
- Added a shared bounded positive-result DNS cache for gateway routing, chain tests
  and health checks. Its configurable TTL is an upper bound because Go's resolver
  interface does not expose authoritative record TTLs; source refresh keeps its
  uncached resolver at the separate SSRF boundary.
- Added an opt-in persistent disk response cache behind the same bounded cache
  contract. Atomic private files survive restart; invalid, corrupt and expired
  entries fail closed, while Admin and CLI stats/purge work for either backend.
- Full CACHE policy semantics and THROTTLE/MOCK/REDIRECT/REWRITE completion remain
  pending, so M9 stays open.

### Original milestone entry gate

Read PLAN.md and the original plan completely, inspect current code and tests,
verify M0 evidence, then start M1. Do not build gateway or dashboard before their
dependencies. Record refinements R1/R2/R5/R8 in the M1 domain/config tests.
