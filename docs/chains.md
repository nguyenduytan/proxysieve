# Proxy Chains

ProxySieve stores proxy chains as revisioned, ordered routing inventory. Each
chain contains 2 to 8 enabled pool references. Every hop is mandatory and has a
configurable timeout; zero uses the 15-second default. A failed hop closes the
partial tunnel and never skips forward or silently routes DIRECT.

```yaml
chains:
  - id: corporate-to-residential
    name: Corporate to residential
    enabled: true
    hops:
      - pool: corporate
        timeout: 10s
      - pool: residential-ca
        timeout: 20s
```

Hop pools, including their reachable fallback pools, must have disjoint endpoint
membership. Sticky session policies on chain hop pools are rejected because one
affinity record cannot correctly represent multiple ordered proxy selections.

Policies can declare ordered alternative chains without changing chain inventory:

```json
{"type":"chain","chain_id":"primary-chain","fallback_chain_ids":["secondary-chain"]}
```

Before any response is delivered, a safe retry first rebuilds the primary chain
with different eligible endpoints. If that is unavailable, ProxySieve tries each
listed fallback chain in order. Every hop remains mandatory.

The Admin Chains workspace supports revisioned create, edit, reorder, enable,
disable and delete operations. Saved changes remain staged until the complete
inventory is activated from Policies. Operators can test the exact active chain
revision against a host and TCP port; the latest in-process result reports
healthy/unhealthy status, end-to-end connection latency and a sanitized failed
hop/pool/proxy identity. Test results reset on process restart and no application
payload is sent.

Traffic events and retained aggregates carry `chain_id`, so chain traffic can be
filtered without counting the same end-to-end stream once per hop. Configured
cost is intentionally absent for chain routes in this foundation: the current
single-rate event model cannot represent multiple hop rates or currencies without
misstating cost. Per-hop configured-cost snapshots are required before chain cost
analytics can be claimed.
