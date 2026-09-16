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

Schema 19 stores usage by budget and window start. Existing lifetime usage upgrades
under window start zero without resetting the guard. Crash-left reservations remain
charged to their original window during startup recovery.

Authenticated viewers can inspect the active configuration and current
used/reserved/remaining bytes through `GET /api/v1/budgets` or the Admin Budgets
workspace. Calendar rows expose their current half-open UTC bounds; lifetime rows
have no reset timestamp. This surface is read-only.

Rolling windows, soft threshold notifications, cost-denominated limits, API/UI
CRUD management, throttle, pool switching and fallback-policy actions remain
required before M8 is complete.
