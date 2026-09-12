# Proxy Sources

ProxySieve accepts manual parser/import previews, persists revisioned source
definitions, and exposes a safe HTTP(S) source refresh API. A refresh first
validates an explicit URL, resolves it, applies destination policy to every
resolved address, then dials a validated address. It sends no ambient cookies or
credentials, does not follow redirects, and caps the response at 8 MiB.

The fetch primitive is intentionally not a general web client. Source URLs must be
HTTP or HTTPS, may not contain userinfo, and must use an operator-approved network
path. A redirected source must be changed deliberately by the administrator; it
will not silently follow to a private/cloud-metadata address.

After fetch, ProxySieve parses the bounded body before opening a database
transaction. `POST /api/v1/sources/{id}/refresh` requires an operator session,
session-bound CSRF token and the current positive source revision. Only enabled
`api` sources are supported in this slice. Production refreshes deny loopback,
private, link-local, multicast and unspecified resolved addresses.

The endpoint reconciliation and successful refresh status update commit in one
SQLite transaction. New endpoint records receive the source ID; matching records
retain their operator-managed fields and are only updated when source ownership
must be attached. Repeated refreshes skip already-associated endpoints, and
missing entries are not deleted. Fetch or parse failures leave endpoint inventory
untouched and record only a bounded, non-sensitive failure status when the source
revision is still current.

The runtime scheduler scans source IDs through a bounded rotating cursor and
refreshes enabled `api` sources whose positive `refresh_interval` has elapsed.
Sources with a zero interval remain manual-only. Each scheduled source receives a
separate timeout, each run caps pages and refresh attempts, and one failed source
does not prevent later due sources from running. Manual and scheduled refreshes
share a per-source coordinator, so the same source is never fetched concurrently
inside one ProxySieve process. Scheduled success/failure audit events contain only
the source ID and stable action name.
