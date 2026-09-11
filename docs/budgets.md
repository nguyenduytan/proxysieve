# Hard Budget Foundation

Hard budgets use synchronized byte reservations. Before a bounded transfer starts,
each applicable budget scope reserves a maximum allocation atomically. Consuming a
lease moves only actually granted bytes from `reserved` to `used`; closing it returns
unused reserved bytes. A lease never grants bytes beyond its reservation.

This avoids the common race where many concurrent tunnels all pass a simple initial
"under budget" check and together exceed a hard cap. A caller must gate stream reads
or writes with the lease allowance. The current manager bounds one reservation to
1 TiB and has no durable restart recovery, policy/API wiring, billing calendar,
threshold alert or route fallback yet. Those are required before a production cost
enforcement claim.
