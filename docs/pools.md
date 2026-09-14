# Pool Inventory

ProxySieve stores routing pools as revisioned inventory in both the in-memory test
adapter and the default SQLite repository. The authenticated Admin API exposes
viewer-safe list/get routes and operator-only create/update/delete routes. Every
mutation is CSRF-protected and recorded in the audit trail.

A pool contains a selection strategy, zero or more proxy endpoint IDs, optional
fallback pool IDs, tag/country/health/latency constraints, an optional sticky
`session_policy`, and an enabled flag. The control plane validates referenced
endpoints and fallbacks before saving, rejects duplicate members and fallback
cycles, and uses optimistic revisions to prevent a stale browser from
overwriting a newer edit. See [sticky sessions](sessions.md) for affinity
limits and durable SQLite-backed runtime scope.

Deletion preserves references: a pool cannot be deleted while another saved pool
uses it as a fallback, and a proxy cannot be deleted while a saved pool includes
it. Reference-sensitive API mutations are serialized within one running server.
Direct repository access is a lower-level persistence boundary and does not apply
cross-resource validation by itself.

The Admin Pools workspace loads all API pages for pools and proxies, supports
create/edit, enable/disable and inline-confirmed deletion, and presents form errors
in the editor instead of duplicating them in the page feedback area. Viewer
sessions receive the same inventory without mutation controls.

## Activation boundary

Persisted pool changes do not alter live routing until an operator activates the
complete inventory from Policies. Activation validates proxy membership, fallback
references/cycles, listener policy references and budget references before it
publishes one immutable revision. API item responses report whether the saved pool
exactly matches the active snapshot. See [runtime activation](runtime-activation.md).
