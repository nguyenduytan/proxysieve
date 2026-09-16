# Durable Hard Budget Foundation

Hard budgets use synchronized byte reservations. Before a bounded transfer starts,
each applicable budget scope reserves a maximum allocation atomically. Consuming a
lease moves only actually granted bytes from `reserved` to `used`; closing it returns
unused reserved bytes. A lease never grants bytes beyond its reservation.

Runtime paid routes use SQLite-backed reservations in chunks no larger than 64 KiB.
HTTP forward, CONNECT and SOCKS5 apply the same configured system/client/pool/proxy
scopes. A hard limit rejects a new paid route at admission; a long-lived stream is
also gated continuously. Direct and cache-only bytes do not consume a paid-route
byte budget.

On clean I/O, actual bytes move from `reserved` to `used` and unused allowance is
returned. On a crash, any reservation left in SQLite is conservatively converted to
used bytes at the next startup. That can over-count up to the in-flight reservation
per lease, but a restart cannot reset or undershoot the hard limit. Database errors
leave reservations charged or reserved and fail closed.

Configuration example:

```yaml
budgets:
  - id: paid-egress
    name: Paid egress safety limit
    scope: system
    limit_bytes: 10000000000
    hard: true
    action: reject
    window: monthly
    timezone: Asia/Saigon
```

Omit `window` for a lifetime guard. Calendar guards accept `daily`, `weekly`, or
`monthly` and require an explicit IANA `timezone`; `Local` is rejected so host
settings cannot silently change enforcement. Daily windows start at local midnight,
weekly windows start Monday at local midnight, and monthly windows start on day 1.
Bounds are persisted as UTC instants and follow timezone DST transitions, so a local
day may contain 23 or 25 elapsed hours. A bounded reservation is charged to the
window in which it was acquired.

Rolling guards use elapsed UTC time and require `window: rolling` plus
`rolling_seconds` from 60 seconds through 365 days. They do not accept a timezone.
Usage is stored in minute buckets and the cutoff minute remains charged until the
next minute boundary. Enforcement can therefore be conservative by less than one
minute, but never expires usage early or depends on lossy traffic analytics.

Schema 19 stores usage by budget and window start. Existing lifetime usage upgrades
under window start zero without resetting the guard. Crash-left reservations remain
charged to their original window during startup recovery.

Schema 20 adds the revisioned budget inventory. On the first start after migration,
the configured YAML budgets are imported atomically. SQLite is authoritative after
that initialization, including when every budget has been deleted, so a restart
does not silently recreate removed limits. Older binaries continue to read YAML and
leave the new inventory untouched, providing a non-destructive rollback path.

Authenticated viewers can inspect configuration and current used/reserved/remaining
bytes through `GET /api/v1/budgets`, `GET /api/v1/budgets/{id}/usage`, or the Admin
Budgets workspace. Operators can create, update and delete budgets with CSRF
protection and optimistic revisions. Mutations are durable and affect new
reservations immediately; leases already in flight finish with their captured
configuration. Historical usage rows remain after deletion. Client, pool and proxy
records cannot be deleted while a scoped budget references them.

Soft threshold notifications, cost-denominated limits, transport framing, pool
switching and fallback-policy actions remain required before M8 is complete.
