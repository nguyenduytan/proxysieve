# Runtime Activation and Rollback

ProxySieve starts from the validated YAML routing resources unless SQLite contains
a previously activated runtime snapshot. A persisted active snapshot wins on
restart, so an operator activation is durable and does not silently revert.

Saving a proxy, pool or policy only changes staged inventory. `POST
/api/v1/runtime/activate` reads all three inventories in one SQLite read
transaction, validates them as one candidate configuration, persists a new
revision and publishes it atomically. The request requires operator access, CSRF
protection and the current `expected_revision`. Stale writers receive `409`.

Validation includes listener policy references, proxy and fallback pool
references, fallback cycles, policy action pool references and configured budget
scope references. Failure leaves the current last-known-good revision unchanged.
Every request carries its evaluation revision into route selection, preventing a
concurrent activation from combining an old policy decision with new pool data.

`POST /api/v1/runtime/rollback` republishes a retained historical bundle as a new
revision. History remains monotonic; rollback never rewinds or edits an old
snapshot. SQLite and the in-process routing manager retain the latest 100 runtime
snapshots. Older targets are unavailable after retention advances.

Runtime changes affect new requests. Existing HTTP requests or established tunnels
continue using the routing objects already selected for them. Snapshots contain
credential references, not resolved secret values. The API exposes revision,
source, actor, timestamp and resource counts rather than raw snapshot documents.
