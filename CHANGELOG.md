# Changelog

## Unreleased

### Added

- Authenticated responsive Admin Panel with API-backed Overview, Traffic, Proxies,
  Sources, Clients, Audit and System destinations; duplicate/inert Alerts
  navigation was removed.
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

No gateway or dashboard is released. See PROGRESS.md for verification status.
