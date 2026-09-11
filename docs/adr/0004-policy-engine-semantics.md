# 0004 — Deterministic policy and route semantics

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

Gateway adapters, simulator/API, and browser integrations must not disagree on
which policy applies. Tunnel requests lack path/header/resource visibility.

## Decision

Evaluate enabled rules by descending priority and stable document order. A condition
can be match, no-match, or unknown. Unknown never matches a rule; it appears in an
optional trace. Conditions compose with all/any/not. The evaluator is pure and route
selection happens afterwards. A terminal route is block, reject, direct, proxy,
cache, mock, redirect, or rewrite; unavailable actions fail at the adapter/router.

`DIRECT` requires an explicit policy action and safe pinned destination resolution.
`PROXY` references an existing pool. A pool failure rejects the request; it never
silently changes to direct. HTTP and SOCKS use the same evaluator/router contract.

## Consequences

Runtime currently supports policy document config but has no admin simulator/API.
Policy validation rejects unknown pool references and bad fallback cycles. HTTPS
tunnels expose host/port only. Advanced path/header/resource actions remain pending.

## Alternatives considered

Embedding matching/routing separately in each listener causes drift and can turn
unobservable tunnel attributes into accidental bypasses.
