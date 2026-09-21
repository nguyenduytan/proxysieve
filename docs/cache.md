# Cache

ProxySieve has separate DNS and HTTP response caches. Both are bounded and neither
changes the fail-closed routing policy.

The DNS cache keeps positive results for the configured TTL and evicts
deterministically at its entry bound. Destination safety is checked on resolved
addresses; caching does not authorize a private destination or prove where a
remote-resolving upstream proxy connects.

The response cache is disabled by default. Enabling its memory or disk backend is
not enough to cache traffic: the matching visible-HTTP policy must also request
`CACHE` and terminate in a supported route. CONNECT/SOCKS tunnels and uninspected
HTTPS are not response-cacheable.

Eligibility is conservative. Requests or responses involving authorization,
cookies, `no-store`, unsupported `Vary`, partial responses, unknown freshness,
oversized/streaming bodies or a non-GET method bypass storage. Keys partition by
the applicable client, session and route context. Disk entries stay under the
data directory with private permissions and survive restart; corrupt or expired
entries are discarded.

Admin shows process-lifetime cache statistics. Operators can purge all entries or
an exact hostname through Admin or `proxysieve cache`; every mutation is audited.
Traffic views label cache-served bytes as exact savings and keep estimates
separate. See [configuration](configuration.md) and [traffic accounting](traffic-accounting.md).

