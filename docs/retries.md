# Retry Safety Foundation

The retry contract is deliberately conservative: retry only connection, timeout or
reset failures before any response has reached the client. GET/HEAD/OPTIONS are
eligible without a body. PUT and DELETE require a replayable body. POST is rejected
unless a future caller explicitly enables an idempotency-key policy and provides a
replayable body. Response status failures are not retried by this foundation.

The transport integration, jitter/backoff, per-request cost budget and failover
selection remain later work. ProxySieve never blindly retries a checkout-like POST.
