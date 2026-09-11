# 0005 — Non-MITM Default and Destination Controls

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

ProxySieve handles HTTP forward traffic, HTTPS CONNECT and SOCKS TCP tunnels. It
must not claim visibility it does not have or leak host traffic on upstream failure.

## Decision

HTTPS inspection is disabled and not implemented in the normal gateway. CONNECT and
SOCKS evaluate only observable host/port/client/listener metadata. Direct routes
resolve first, reject unsafe result sets for untrusted traffic, then dial a validated
IP rather than allowing a second hostname lookup. This prevents local DNS check/dial
races. Private, loopback, link-local, unspecified and multicast destinations are
denied by default.

Upstream proxy DNS is inherently a trust boundary. With private-destination denial
enabled, a configured HTTP/HTTPS/SOCKS endpoint must explicitly declare
`trusted_remote_dns: true` before proxy routing is allowed. The operator accepts
responsibility for its documented upstream enforcement contract.

## Consequences

Proxy failures reject rather than route direct. Not all commercial proxies can be
used under the strict default until their remote DNS behavior is assessed. Optional
inspect mode will be a later explicitly scoped module with its own CA handling.
