# 0001 — Go core and milestone-scoped bootstrap

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

ProxySieve needs portable networking, bounded concurrency, predictable deployment,
and small adapter boundaries. The repository has no existing implementation.

## Decision

Use the plan's Go 1.27 baseline (bootstrap verified with 1.27.1) and the remote's
module path, github.com/nguyenduytan/proxysieve. M0 uses the standard library only.
Keep process exit in cmd, CLI behavior testable, and build provenance separate.
Pin the React 19.3/Vite 8.1 tooling workspace without starting the M12 dashboard.
PLAN.md links the preserved original contract and tracked refinements rather than
duplicating the original specification. Credit Tony Nguyen in project metadata.

## Consequences

M0 provides no listeners, database, API, or functioning proxy. Unsupported start
fails explicitly. No go.sum exists until dependencies are introduced; CI caching
must reflect this. SQLite, secrets, policy, and extension decisions get their own
ADRs when their boundaries are actually designed. No empty abstraction tree.

## Alternatives considered

A premature dashboard/mock API hides missing foundations. Adding a full gateway
in the bootstrap bypasses protocol/security gates. Duplicating the large plan
invites drift. None are adopted.
