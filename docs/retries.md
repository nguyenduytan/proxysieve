# Retry Safety Foundation

The retry contract is deliberately conservative: retry only connection, timeout or
reset failures before any response has reached the client. GET/HEAD/OPTIONS are
eligible without a body. PUT and DELETE require a replayable body. POST is rejected
unless a future caller explicitly enables an idempotency-key policy and provides a
replayable body. Response status failures are not retried by this foundation.

`retry.max_attempts` configures one to five total attempts, including the first;
one disables retries. `retry.allow_idempotency_key` is off by default. When enabled,
a bodyless request with an `Idempotency-Key` may use the same bounded retry path.
Backoff remains exponential with jitter. Every replacement still passes health,
hard-budget and half-open checks, and never falls back to `DIRECT`.

Current transport adapters retry bodyless idempotent HTTP methods and tunnel dials
made before a success reply. They do not replay any body-bearing request even when
the standalone policy contract could permit a known replayable body. Each attempt
has its own route/proxy traffic event while retaining the request and connection IDs
that tie the attempts together. A failed pre-response tunnel dial records zero
application-stream bytes; ProxySieve never blindly retries a checkout-like POST.

A chain action may provide up to 15 ordered `fallback_chain_ids`. Retry first tries
the primary chain with different eligible endpoints, then each fallback chain. A
fallback chain is never entered after response bytes have reached the client.
