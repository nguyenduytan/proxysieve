# Changelog

## Unreleased

### Added

- Authenticated responsive Admin Panel with API-backed Overview, Traffic, Proxies,
  Sources, Pools, Chains, Sessions, Policies, Clients, Audit and System
  destinations; duplicate/inert Alerts navigation was removed.
- Bounded asynchronous traffic persistence, HTTP/CONNECT/SOCKS5 application-stream
  accounting, tiered rollups/retention, configured-cost snapshots and durable hard
  byte budgets with restart-safe reservations.
- Audited client/API-key lifecycle endpoints, one-time raw token creation, list and
  revoke operations, plus an embedded OpenAPI contract at `/api/v1/openapi.yaml`.
- Added `proxysieve doctor` with text/JSON diagnostics for configuration, data
  directory, SQLite schema and listener-bind readiness.
- Added authenticated proxy inventory GET/PATCH/DELETE lifecycle endpoints with
  optimistic revision conflicts and audit events.
- Added atomic proxy inventory imports with bounded parsing and duplicate modes.
- Added revisioned proxy-source persistence and audited, RBAC-protected CRUD API.
- Added SSRF-safe, revision-checked proxy-source refresh with atomic endpoint reconciliation and bounded failure status.
- Added bounded automatic source scheduling with fair cursor rotation, per-source timeouts and overlap prevention shared with manual refresh.
- Added a responsive Admin Sources workspace for viewer-safe listing and
  operator CRUD, enable/disable, manual refresh and schedule management.
- Added admin-only Clients & API keys workspace with revisioned client CRUD,
  enable/disable, one-time token reveal/copy, key listing/revocation and
  destructive client confirmation.
- Added an admin-only read-only Audit workspace backed by sanitized audit events,
  with refresh state, empty/error handling and responsive mobile cards.
- Added stable timestamp/id cursor pagination to the Audit API and Load more UI.
- Added revisioned proxy-pool persistence and audited RBAC/CSRF-protected CRUD API,
  including endpoint/fallback validation, fallback-cycle checks and reference-safe
  deletion.
- Added bounded process-local sticky affinity for explicit, client, destination
  and client-plus-destination session keys, with TTL/idle/request/byte rotation,
  health and runtime-revision invalidation, cache partitioning, internal-header
  isolation and Admin pool controls.
- Added viewer-safe session list/detail APIs and a responsive Sessions workspace,
  plus CSRF-protected audited operator rotation/deletion without interrupting
  active tunnels or exposing raw affinity keys.
- Added revisioned 2-to-8-hop proxy chains with ordered mandatory routing,
  per-hop timeouts, runtime activation/rollback, active probes, audited CRUD,
  Admin controls and chain-attributed traffic events/rollups.
- Added a responsive Pools workspace for viewer-safe inventory and operator
  create/edit, enable/disable and inline-confirmed delete workflows with explicit
  staged-state semantics.
- Added revisioned policy persistence and audited policy CRUD with strict
  condition/action validation, pool-reference protection and canonical evaluator
  simulation. Added a responsive Policies workspace with JSON authoring,
  optimistic revisions, simulation results and inline-confirmed deletion without
  coupling Save actions to runtime activation.
- Added fail-closed SQLite backup and restore commands with WAL-consistent snapshots,
  schema validation, no implicit overwrite, timestamped pre-restore archives,
  WAL/SHM sidecar handling and atomic database replacement.
- Added durable, optimistic runtime activation and rollback for complete proxy,
  pool and policy inventories. Activation uses one SQLite read snapshot, preserves
  last-known-good state on validation failure, survives restart and keeps policy
  evaluation paired with route selection from the same runtime revision.
- Added Admin runtime status, explicit activation confirmation and retained
  revision rollback without introducing a duplicate alert surface.
- Added synchronized/staged runtime indicators, exact active/staged item state,
  rollback confirmation and disabled no-op activation. Fixed default policy ID
  authoring and desktop/mobile policy action layout.
- Added rolling health outcome/status signals, proxy-auth and timeout
  classification, globally/per-pool paced active and manual checks, and a
  responsive Health workspace with guarded operator actions.
- Completed revisioned client GET/PATCH/DELETE API routes with cascade key
  cleanup, RBAC/CSRF enforcement, audited mutations and OpenAPI schemas.
- Connected Admin proxy import commits and responsive, revision-safe
  enable/disable controls.
- M1 domain contracts, bounded YAML configuration with provenance and CLI inspection.
- SQLite migrations and endpoint repositories with revision-based concurrency,
  transaction rollback, keyset pagination and matching in-memory behavior.
- Redacted secret values, authenticated-encryption primitives and exact cost values.
- Configuration fuzz target, dependency-boundary and security/regression tests.
- M0 Go CLI with help, version, JSON build metadata, and explicit unavailable start.
- Tony Nguyen creator/maintainer attribution and Apache-2.0 licensing.
- Engineering refinements, milestone tracker, contribution and security documents.
- Frontend tooling workspace, CI definitions, container and release scaffolds.

No public release is published. See PROGRESS.md for verification status.
