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
mock, or redirect. Cache and same-origin path rewrite decorate a visible HTTP
direct/proxy/chain route; throttle decorates HTTP or tunnel streams. Unavailable
actions fail at the adapter/router.

`DIRECT` requires an explicit policy action and safe pinned destination resolution.
`PROXY` references an existing pool. A pool failure rejects the request; it never
silently changes to direct. HTTP and SOCKS use the same evaluator/router contract.

## Consequences

Policy validation rejects unknown pool references, bad fallback cycles and malformed
advanced-action values. HTTPS tunnels expose host/port only and visible-HTTP actions
fail closed there. Header/content rewrite remains outside the current contract.

## Alternatives considered

Embedding matching/routing separately in each listener causes drift and can turn
unobservable tunnel attributes into accidental bypasses.
