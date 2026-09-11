# Accepted engineering refinements

Date: 2026-09-11. Maintainer: **Tony Nguyen**.
Status: implementation requirements accepted under the maintainer's request to
review, optimize, and add necessary safeguards. These are not implemented features.

The original scope is already broad enough. The highest-value additions are
precise behavior, failure contracts, and tests, not another infrastructure layer.

## R1 — Byte accounting and money (M1, M3, M8)

- Define each counter's measurement boundary before exposing it. Count successful
  read/write byte counts, including partial operations that also return errors.
- Distinguish logical HTTP payload, transport bytes (including TLS/proxy framing
  when measured), and provider-billed usage. Do not claim NIC-level/IP/TCP overhead
  or an exact provider bill from application stream counters.
- Retries, health checks, and each paid chain hop consume traffic; attribute them
  separately without double-counting a chain total as an end-to-end payload total.
- Use integer byte counters and fixed-point money, explicit currency, decimal GB
  versus GiB, rounding policy, and effective-dated prices. Never use floating-point
  currency as the persisted authority or sum incompatible currencies.
- Persist timestamps in UTC. Budget calendar/timezone and DST behavior must be
  explicit; rolling windows use elapsed time. Pricing changes must not rewrite
  historical charges.
- Gate: partial I/O, retry/chain attribution, overflow, rounding, price changes,
  timezone rollover, and restart reconciliation tests.

## R2 — Concurrent hard budgets (M6–M8)

- Admission checks alone cannot constrain long-lived tunnels. Reserve bounded
  byte allowances atomically across all applicable budget scopes, reconcile actual
  consumption, and return unused reservations on cancellation/failure.
- Enforce ongoing transfer limits and document maximum possible overshoot from
  in-flight buffers. Never describe a stream allowance as an exact provider cap.
- Hard-budget state cannot depend on lossy analytics events. Define restart/crash
  recovery and reject new paid traffic when required enforcement state is unsafe.
- Gate: many concurrent tunnels near a limit, process restart, reservation cleanup,
  multiple scopes, and bounded overshoot tests.

## R3 — Protocol integrity and credential boundaries (M3–M5)

- Reject ambiguous HTTP framing and conflicting target/authority forms; handle
  Content-Length/Transfer-Encoding consistently. Use the standard HTTP parser and
  explicit defensive checks instead of inventing a permissive parser.
- Strip hop-by-hop fields including fields nominated by Connection. Never forward
  downstream Proxy-Authorization or internal client/session metadata to an origin.
  Never send one upstream's credentials to another upstream or across redirects.
- Reject self-routing/proxy loops, enforce chain depth, and test IPv6 forms,
  half-close, early disconnect, Expect: 100-continue, and bounded header parsing.
- Document WebSocket upgrade, HTTP/2, and HTTP/3 support by transport mode. Opaque
  CONNECT can carry protocols the visible-HTTP adapter cannot inspect. Unsupported
  visible protocol modes must fail explicitly, not silently change semantics.
- Gate: adversarial framing, auth-isolation, loop, upgrade, and stream fixtures.
- Basis: [RFC 9112](https://www.rfc-editor.org/rfc/rfc9112.html).

## R4 — DNS enforcement and outbound control-plane safety (M2–M4, M13)

- Check all resolved addresses; normalize IPv4-mapped IPv6 and other address forms.
  Bind direct/local-resolution dials to validated addresses to avoid check/dial
  races. Reject unsafe mixed public/private results for untrusted traffic.
- Local DNS validation does not prove where a remote-resolving proxy will connect.
  For untrusted clients require a transport that pins a validated destination, or
  an explicitly trusted upstream enforcement contract; otherwise reject the route.
  Remote-DNS mode must disclose its trust requirements and potential DNS leakage.
- Source refresh, webhooks, diagnostics, and redirect following need their own
  destination validation, scheme restrictions, redirect/depth caps, body limits,
  and credential-forwarding policy. They are SSRF surfaces too.
- Gate: rebinding/mixed DNS, redirected source/webhook to private IP, IPv6, and
  upstream-resolution uncertainty tests.

## R5 — Control-plane consistency and recovery (M1, M5, M8, M11, M15)

- Validate a complete candidate configuration, publish one immutable revision
  atomically, and retain last-known-good state on failure. Never partially apply
  pools and policies from different revisions. Expose revision and rollback.
- Use optimistic concurrency (ETag/revision preconditions) for admin edits; reject
  stale updates instead of silently overwriting another operator's work.
- Define mutation idempotency before implementing bulk import/rotation/restore.
- Separate best-effort traffic feeds from durable audit and budget enforcement.
  Bounded queues need observable loss/backpressure policies, not silent drops.
- Explicitly define behavior for database locked/full/corrupt, secret-store failure,
  shutdown drain timeout, and a second process opening the same data directory.
- Gate: failed reload, concurrent edits, disk/write failure, restart, and drain tests.

## R6 — Cache and privacy isolation (M9, M14)

- Default to bypass for cookies, Set-Cookie, Authorization, private/no-store,
  partial responses, unknown Vary semantics, and streaming content unless the
  relevant case has an explicit, tested eligibility contract.
- Cache keys must respect Vary/content encoding and partition by applicable
  client/session/route/security context; identical URLs can have different content
  for different identities, exit regions, or policies. Never cache a personalized
  response globally merely because its URL ends in a static-looking extension.
- Specify GET/HEAD, conditional revalidation, invalidation, eviction, and stale
  behavior. Do not decompress unbounded bodies for inspection or accounting.
- Logs must redact query-string secrets as well as headers/userinfo. Diagnostic
  bundles are opt-in, sanitized, bounded, and previewable before export.
- Gate: cross-client/session leakage, Vary, cookies, revalidation, compression
  expansion limits, query redaction, and retention tests.
- Basis: [RFC 9111](https://www.rfc-editor.org/rfc/rfc9111.html).

## R7 — Browser adapter trust and lifecycle (M10)

- Define precedence against existing request handlers, attach/detach idempotency,
  policy snapshot expiry, offline behavior, and batched bounded block reporting.
- Explicitly test/document Service Worker interception and WebSocket behavior.
  Browser-side hooks alone cannot promise that every browser transport is filtered.
- Gateway security/budgets remain authoritative. Client-supplied resource type and
  local-block reports are untrusted hints, not permission to bypass security or
  proof of measured upstream savings.
- Strip integration metadata at the gateway boundary and document whether sticky
  credentials remain scoped to a BrowserContext rather than a tab.
- Gate: Service Worker fixtures, concurrent contexts, existing interception,
  expired/offline snapshots, and forged metadata tests.

## R8 — Secret lifecycle, setup, and extensions (M1, M11, M13–M15)

- First-run setup must be one-time, short-lived, resistant to races, and separate
  from normal login. Protect admin Host/Origin to address localhost DNS rebinding;
  CORS alone is insufficient. No default password or persistent setup-token log.
- Specify key revocation, credential rotation, active-tunnel revocation behavior,
  and cache invalidation latency. Test immediate denial of new connections.
- Define master-key recovery/loss and encrypted-backup portability. Test restrictive
  Windows ACLs as well as Unix modes; chmod-only checks are not cross-platform.
- External process crash isolation is not a security sandbox. Installing/running
  extensions is admin-only trusted-code execution. Do not auto-download/execute
  extensions; bound protocol frames, stderr, restarts, deadlines, and capabilities.
- Gate: setup races, revoked keys, changed secret versions, unsafe ACLs, lost key,
  extension crash loops/oversized messages, and encrypted restore tests.

## R9 — Delivery and identity (M0, M12, M16)

- Use the real remote module path: github.com/nguyenduytan/proxysieve.
- Credit **Tony Nguyen** in README, NOTICE, CLI version metadata, package metadata,
  and the dashboard About/footer when M12 ships. Preserve contributor recognition;
  do not add a custom restrictive license clause or inject branding into traffic.
- Pin verified released tooling and keep lockfiles. Install only tooling needed
  for the current milestone. Keep an honest capability/status matrix.
- Do not require nonexistent CI jobs, publish a fake v1 release, or add mandatory
  external services. Record pending hosted/Docker checks separately from local tests.
- Baselines verified at bootstrap: Go 1.27.1, React 19.3.0, Vite 8.1.5,
  Node 24 LTS, pnpm 11.19.0. Preserve the plan's supported minor baselines.

## Prioritization

R1–R5 and R8 are correctness/security gates before the affected feature ships.
R6–R7 refine already-planned optimization features. R9 applies immediately.
PostgreSQL, Redis, distributed gateways, AI routing, and a provider marketplace
remain outside v1 core. Performance changes require benchmark evidence.
