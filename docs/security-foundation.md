# Security foundation (M1)

`pkg/secret.Value` hides raw bytes from normal fmt formatting (including `%#v`), JSON,
text encoding and slog. `Reveal()` is an explicit copy-returning transport/crypto
boundary; it must not feed logs. This is a guard against accidents, not a mechanism
for preventing trusted code or a memory debugger from reading process memory.

Endpoint and provider contracts use secret references. The in-memory secret store
is bounded and ephemeral. AES-256-GCM envelopes authenticate the reference as
associated data and generate a fresh nonce per encryption. Tampered data, another
reference, another key or unsupported envelope version fails authentication.

The cipher is a primitive, **not yet a persistent secret store**. Key generation,
Windows ACL/Unix permission enforcement, rotation before 2^32 messages per key,
master-key recovery, restart persistence and encrypted exports still require their
own implementation/testing before live credentials are accepted.

Header redaction returns a copy. URL diagnostic sanitization drops userinfo and all
query/fragment values, including unknown query keys. URL paths themselves can carry
secrets, so raw URLs and request contexts are not log-ready DTOs. Metadata-only
logging and allowlisted structured event fields remain the default runtime design.

SQLite stores endpoint metadata and secret references, not raw upstream credential
values. Local file permissions alone are not claimed to protect credentials on
Windows. Do not use production credentials in the unreleased build or commit them
in tests.

The admin control plane stores Argon2id password hashes and identity metadata. Its
setup token is memory-only/console-only, session tokens are hashed before lookup,
mutations require same-origin CSRF validation, and roles gate operations. API keys
are displayed once and stored only as hashes; audit records contain sanitized
metadata. Browser adapters send those keys only to a loopback control URL, keep
session metadata in block reports rather than origin headers, and fail open when a
safe local policy decision cannot be made. Admin TLS, encrypted persistent
upstream-secret storage,
role-managed user CRUD and explicit audit retention remain control-plane work.
