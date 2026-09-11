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

SQLite stores endpoint metadata, not raw secret material. Local file permissions
alone are not claimed to protect credentials on Windows. Do not use production
credentials in this foundation build or commit them in tests.
