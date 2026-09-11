# 0002 — SQLite foundation with replaceable typed repositories

Status: accepted, 2026-09-11. Maintainer: Tony Nguyen.

## Context

The plan requires one-binary deployment, SQLite persistence, in-memory tests, and
transactional migration. Core contracts must not contain driver/SQL types.

## Decision

Use modernc.org/sqlite 1.58.0 behind internal/storage/sqlite (pure Go, no cgo).
Expose typed endpoint CRUD, keyset pagination and a transaction callback through
pkg/store. Both memory and SQLite run the same behavior suite. Writes require an
expected revision; create uses zero. IDs sort bytewise and list sizes are bounded.

M1 persists endpoint metadata as a validated bounded JSON document with relational
ID/revision constraints and a host expression index. This is not a generic arbitrary
document store. More normalized relationships/indexes arrive with source/pool/query
requirements in M2/M6, through migrations, not schema-less writes or ORM auto-migrate.

SQLite uses WAL, foreign keys, FULL synchronization, a bounded busy timeout and one
database/sql connection per adapter. An IMMEDIATE transaction serializes migrations;
embedded migration names/checksums reject modified or newer histories.

## Consequences

M1 favors simple correctness over high write concurrency. Transaction callbacks
must use the supplied repository synchronously and not re-enter the parent store.
The memory transaction copies bounded metadata; it is not a network hot-path store.
Database errors are sanitized. Metadata storage is not encrypted secret storage.
Raw credentials are prohibited; only SecretRef is part of the endpoint model.

Parent directories must already exist. SQLite handles multiple database connections,
but enforcing a single gateway process per data directory belongs to runtime wiring
in M3. Filesystem ACL/master-key lifecycle is not claimed implemented by this ADR.

## Alternatives considered

A cgo-only driver adds cross-build complexity. A generic untyped CRUD API weakens
domain contracts. Creating every final table now would lock in untested assumptions.

## References

- https://pkg.go.dev/modernc.org/sqlite
- https://www.sqlite.org/lang_transaction.html
