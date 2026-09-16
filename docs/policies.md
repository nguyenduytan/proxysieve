# Policy Inventory and Simulation

ProxySieve stores policy documents as revisioned inventory in memory and SQLite.
The authenticated Admin API exposes viewer-safe list/get/simulation routes and
operator-only create/update/delete routes. Mutations require CSRF protection,
optimistic revisions and produce sanitized audit events.

## Document boundary

Policy documents currently use version `1`. Every rule needs a unique valid ID,
a name, at least one supported action and a bounded condition tree. Conditions
support the observed request fields `host`, `listener`, `client`, `protocol`,
`scheme`, `method`, `path`, `resource_type`, `destination_ip` and `hour_utc`.
Supported operators are `equals`, `any`, `suffix`, `wildcard`, `regex` and
`cidr`; regex, wildcard and CIDR values are compiled or parsed during validation.

Proxy actions must reference a saved pool. A pool referenced by any saved policy
cannot be deleted until the policy reference is removed. Pool and policy
reference checks are serialized within the running control plane so concurrent
mutations cannot bypass those checks.

Chain actions require `chain_id` and may add up to 15 unique ordered
`fallback_chain_ids`. Retry exhausts eligible replacement endpoints in the primary
chain before trying those alternatives. Primary and fallback chains cannot be
deleted while a saved policy references them, and no failure path substitutes
`DIRECT`.

## Simulation

`POST /api/v1/policies/{id}/simulate` accepts a bounded request context and runs
the same canonical evaluator used by the policy package. It returns the outcome,
actions, matched rule IDs, per-condition traces and fields that were unavailable
to the simulation. Unknown fields remain unknown; they do not accidentally match
empty input values.

Simulation is read-only with respect to routing. Responses include
`simulation_only: true` and reports whether the saved policy exactly matches the
active runtime document. The endpoint does not dial a
proxy, alter live sessions or activate the saved document.

## Admin workspace

The Policies workspace is available to viewers as read-only inventory. Operators
and administrators can author a complete JSON document, edit it with an optimistic
revision, run the fixed sample simulation and delete it with inline confirmation.
List feedback and editor validation are kept as separate surfaces so one action
does not produce duplicate alerts.

The Policies workspace exposes active revision/counts, explicit activation and
rollback. Activation validates and publishes the complete saved proxy, pool and
policy inventory as one immutable revision; individual Save actions never alter
live routing. Existing requests retain the revision they evaluated against, while
new requests use the newly active revision. See [runtime activation](runtime-activation.md).

A visual rule builder, durable simulation history and SSE event streaming remain
later milestone work.
