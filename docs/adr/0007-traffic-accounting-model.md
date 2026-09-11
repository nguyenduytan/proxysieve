# 0007 — Exact byte boundaries and fixed-point cost snapshots

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

Payload, socket bytes, estimated avoided bytes and a provider's billing are not
interchangeable. Floating-point totals and per-chunk rounding can distort budgets.

## Decision

Use checked uint64 byte values and int64 currency micros (one million per unit),
with explicit three-letter currency, GB/GiB basis and effective-dated rate snapshots.
Charge aggregates using integer multiplication/division and half-up rounding.
Keep counters for client, upstream, direct, cache, health and estimated avoided bytes
separate. Later transport adapters will define exactly where each counter is taken.

## Consequences

M1 implements values and arithmetic, not network accounting or budget enforcement.
Do not sum rounded individual chunks, mutate historical prices, add currencies,
or claim an exact invoice. Provider measurements may differ due to their own
framing/rounding rules. Persist UTC timestamps; budget calendars come with M8.

## Alternatives considered

Floating-point currency and opaque "total savings" counters are easier initially
but do not provide the required reproducibility and honest reporting.
