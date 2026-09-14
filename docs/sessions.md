# Sticky Sessions

Pools can opt into bounded sticky-session affinity through `session_policy`.
The policy is part of the revisioned pool inventory and is applied only after
the complete inventory is activated.

Supported strategies are:

- `none`: select a proxy for every request.
- `explicit`: use the downstream `X-ProxySieve-Session` value as the affinity
  key. The header is consumed by the gateway and never reaches policy-visible
  origin headers or an upstream proxy.
- `client`: bind by authenticated downstream client identity.
- `destination`: bind by normalized destination host.
- `client_destination`: bind by both client identity and destination host.

`ttl_ns`, `idle_ttl_ns`, `max_requests`, and `max_bytes` bound a session. A
zero value disables that limit. Keys are retained only as HMAC digests; raw
session values are not stored or returned.

Sessions are currently process-local, bounded runtime state. A process restart
starts a new affinity namespace, while the active routing revision remains
restart-safe in SQLite. The Admin API does not yet expose session listing or
manual rotation, so session observability and durable session services remain
release work rather than being implied by the pool editor.

Affinity is resolved before each new request or tunnel. An active tunnel is not
reassigned mid-stream. Runtime revision changes, policy changes, expiry,
limits, endpoint health quarantine, endpoint removal, or an upstream failure
mark a binding rotated; the next request selects a fresh eligible endpoint.
