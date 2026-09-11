# 0009 — Local development while publication is gated

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

All M0 local checks pass, but publication to the public GitHub repository requires
specific authorization. The maintainer requested continued development while away.

## Decision

Continue milestone-scoped local implementation and review on an unpublished branch.
Do not attempt another push, alter repository visibility, or use another transport
to publish files. Keep hosted CI/container acceptance explicitly pending. Only
locally verified capabilities may be described as locally tested.

## Consequences

The repository can progress safely without exporting files. M0 and final release
acceptance remain incomplete until hosted checks run. Local commits are checkpoints,
not releases or evidence that GitHub checks passed.

## Alternatives considered

Blocking all code work needlessly prevents authorized local work. Treating a broad
continuation as a bypass for a denied publication would violate the approval boundary.
