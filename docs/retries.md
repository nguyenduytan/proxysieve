# Retry Safety Foundation

The retry contract is deliberately conservative: retry only connection, timeout or
reset failures before any response has reached the client. GET/HEAD/OPTIONS are
eligible without a body. PUT and DELETE require a replayable body. POST is rejected
unless a future caller explicitly enables an idempotency-key policy and provides a
replayable body. Response status failures are not retried by this foundation.

The HTTP, CONNECT and SOCKS5 adapters make at most two total attempts. A failed
attempt can select a different eligible endpoint from the same or configured
fallback pool after bounded jittered backoff. The replacement still passes health,
hard-budget and half-open checks, and never falls back to `DIRECT`.

Current transport adapters retry only bodyless GET/HEAD/OPTIONS requests and tunnel
dials made before a success reply. They do not replay PUT, DELETE, POST or any other
body-bearing request even when the standalone policy contract could permit a known
replayable body. Separate traffic/cost attribution for each failed attempt remains
required before M7 acceptance; ProxySieve never blindly retries a checkout-like POST.
