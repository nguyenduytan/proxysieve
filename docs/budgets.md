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
```

Current limits are lifetime byte guards. Billing calendars, rolling windows, soft
threshold notifications, cost-denominated limits, API/UI management, throttle,
pool switching and fallback-policy actions remain required before M8 is complete.
