# Proxy Sources

ProxySieve accepts manual parser/import previews, persists revisioned source
definitions, and has a safe HTTP(S) source-fetch primitive for future scheduling
integration. A refresh first validates an
explicit URL, resolves it, applies destination policy to every resolved address, then
dials a validated address. It sends no ambient cookies or credentials, does not
follow redirects, and caps the response at 8 MiB.

The fetch primitive is intentionally not a general web client. Source URLs must be
HTTP or HTTPS, may not contain userinfo, and must use an operator-approved network
path. A redirected source must be changed deliberately by the administrator; it
will not silently follow to a private/cloud-metadata address.

After fetch, `PreviewHTTP` uses the standard parser preview. It reports valid,
invalid and duplicate records without persisting endpoints. Authenticated
`/api/v1/sources` CRUD persists bounded non-secret source configuration with
optimistic revisions; refresh timestamps/status remain server-controlled. The
source scheduler, refresh API and endpoint reconciliation remain future slices.
Existing endpoint records are never deleted merely because a refresh fails.
