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

Sessions are bounded durable state when the SQLite-backed control plane is enabled.
ProxySieve stores only lifecycle metadata and HMAC key digests; the process-local
HMAC secret is persisted separately as `session-hmac.key` with restrictive file
permissions so active bindings can be reconstructed after restart. Runtime without
the Admin/SQLite store remains process-local. Authenticated viewers can list and
inspect sessions in the Admin API and Sessions workspace. Operators can manually
rotate or delete a binding; both mutations are CSRF-protected and audited. The raw
key is never returned and the UI displays only a short prefix of its HMAC digest.

The SQLite backup command assumes restore into the same data directory, where the
HMAC key remains available. Moving a database that contains sessions without its
matching `session-hmac.key` fails startup instead of silently creating a new
affinity namespace. Rotate/delete sessions before moving a database, or transfer
that key through a separate protected channel; it is intentionally excluded from
portable config exports.

Affinity is resolved before each new request or tunnel. An active tunnel is not
reassigned mid-stream. Runtime revision changes, policy changes, expiry,
limits, endpoint health quarantine, endpoint removal, or reaching the upstream
failure threshold mark a binding rotated; the next request selects a fresh
eligible endpoint. The session CLI remains release work.
