# Proxy Sources

ProxySieve accepts bounded text, CSV and JSON parser/import previews, persists
revisioned source definitions, and refreshes safe HTTP(S) or local file sources.
CSV uses a header row. JSON accepts an array or a top-level object containing an
array. Flat mapping keys select the array, complete endpoint, protocol, host and
port fields; defaults are `items`, `endpoint`, `protocol`, `host` and `port`.

The HTTP fetch primitive is intentionally not a general web client. Source URLs must be
HTTP or HTTPS, may not contain userinfo, and must use an operator-approved network
path. A redirected source must be changed deliberately by the administrator; it
will not silently follow to a private/cloud-metadata address.

File paths are relative to `<data_dir>/imports`. Absolute paths, traversal,
outside-root symlinks, non-regular files and files larger than 8 MiB are rejected.
The imports directory is created with the data directory when the Admin control
plane starts.

After reading, ProxySieve parses the bounded body before opening a database
transaction. `POST /api/v1/sources/{id}/refresh` requires an operator session,
session-bound CSRF token and the current positive source revision. Only enabled
`api` and `file` sources are refreshable. Production HTTP refreshes deny loopback,
private, link-local, multicast and unspecified resolved addresses.

The endpoint reconciliation and successful refresh status update commit in one
SQLite transaction. New endpoint records receive the source ID; matching records
retain their operator-managed fields and are only updated when source ownership
must be attached. Repeated refreshes skip already-associated endpoints, and
missing entries are not deleted. Fetch or parse failures leave endpoint inventory
untouched and record only a bounded, non-sensitive failure status when the source
revision is still current.

The runtime scheduler scans source IDs through a bounded rotating cursor and
refreshes enabled `api` and `file` sources whose positive `refresh_interval` has elapsed.
Sources with a zero interval remain manual-only. Each scheduled source receives a
separate timeout, each run caps pages and refresh attempts, and one failed source
does not prevent later due sources from running. Manual and scheduled refreshes
share a per-source coordinator, so the same source is never fetched concurrently
inside one ProxySieve process. Scheduled success/failure audit events contain only
the source ID and stable action name.
