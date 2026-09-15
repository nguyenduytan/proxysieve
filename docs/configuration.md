# Configuration foundation (schema v1)

Maintainer: Tony Nguyen. Available now: validation and effective-config inspection.
No command in this document starts the gateway, applies a live reload, or writes
configuration/database files. Gateway/runtime features remain later milestones.

```sh
go run ./cmd/proxysieve config validate --file config.example.yaml
go run ./cmd/proxysieve config print-effective --file config.example.yaml
go run ./cmd/proxysieve config print-effective --set logging.level=debug
```

`--file` is explicit: if omitted, use defaults plus environment/flags, not an implicit
read of a potentially unrelated working-directory file. YAML requires `version: 1`.
Unknown fields, duplicate keys, aliases, merge keys, nulls, multiple documents,
excessive nesting, and documents over 1 MiB are rejected. JSON is accepted as a YAML
subset with the same field/size rules. Durations are strings such as `5s` or `2m`.

Precedence is defaults < file < environment < flags < runtime. Runtime is currently
an internal loader input, not a live API. `print-effective` includes a source map
for scalar/object fields and whole-array replacement. No raw credentials belong
in configuration. References use `secret://namespace/name`, not URI userinfo.

Environment keys currently supported:

| Environment | Canonical field |
| --- | --- |
| PROXYSIEVE_LOG_LEVEL | logging.level |
| PROXYSIEVE_LOG_FORMAT | logging.format |
| PROXYSIEVE_DATA_DIR | server.data_dir |
| PROXYSIEVE_ADMIN_BIND / PROXYSIEVE_API_BIND | admin.bind |
| PROXYSIEVE_STORAGE_DRIVER | storage.driver |
| PROXYSIEVE_STORAGE_PATH | storage.path |

Unknown PROXYSIEVE_ keys are rejected. Conflicting admin-bind aliases are rejected.
Changing data_dir derives a new default database path unless storage.path was set
explicitly. No shell/environment expansion occurs inside YAML values. `~` is not
expanded from user-provided strings; use an explicit path. Built-in defaults use
the OS user-home lookup. Command-line `--set` accepts existing scalar dotted paths;
edit the file for arrays or optional fields. Duplicate `--set` keys are rejected.

Traffic retention defaults to 30 days of raw events, 90 days of minute aggregates,
365 days of hour aggregates, and 3,650 days of UTC-day aggregates. Configure these
with `traffic.retention_days`, `traffic.minute_retention_days`,
`traffic.hour_retention_days`, and `traffic.day_retention_days`. The scheduler
normalizes effective values so each coarser tier lasts at least as long as its
source tier; shortening a setting never moves a durable watermark backward or
recovers already-pruned data. `traffic.aggregation_interval` controls maintenance
frequency and defaults to one minute.

`budgets` accepts durable paid-route byte guards. Supported scopes are `system`,
`client`, `pool`, and `proxy`; every non-system scope requires `scope_id`. Pool and
proxy IDs must exist in the same configuration. A hard budget currently requires
`action: reject`. Configured budgets require the SQLite-backed admin/control store;
startup fails rather than silently running an in-memory hard limit.

Listener arrays replace defaults in full. Required listener identity/protocol/auth
fields must be present. Omitted connection limit/idle timeout receive safe defaults;
explicit zero is invalid. Binds must be literal IP:port, with brackets for IPv6.
Unauthenticated non-loopback listeners are rejected. Admin authentication is required;
remote admin needs TLS certificate/key references. No permissive dev bypass exists.

DIRECT requires an explicit nonempty allowlist; inspect requires an include scope.
Validation does not imply the corresponding feature has shipped. Domain patterns,
policy links, actual key access, occupied ports, and runtime capabilities will be
validated by their owning services when those are implemented.

`config.Manager` atomically stores validated immutable revisions and rejects stale
updates. It does not itself restart listeners, persist revisions, or apply effects.
Full config migration/rollback CLI and live reload come with the owning milestones.

## Local Proxy Resources

The runtime also accepts `proxies`, `pools`, `chains`, and `policies`. A policy
route action references a configured pool or chain by ID; a pool lists endpoint
IDs and selection strategy, while a chain lists 2 to 8 ordered pool hops.
ProxySieve does not silently substitute `DIRECT` when a pool is empty, unhealthy,
misconfigured, or its credentials are unavailable.

```yaml
chains:
  - id: corporate-to-residential
    name: Corporate to residential
    enabled: true
    hops:
      - pool: corporate
        timeout: 10s
      - pool: residential-ca
        timeout: 20s
```

Chain hop pools must not share reachable endpoints, including through fallbacks.
Sticky session policies on chain hop pools are rejected until multi-hop affinity
can preserve correct semantics. See [proxy chains](chains.md).

An endpoint may include a deterministic configured price snapshot:

```yaml
rate:
  price: { currency: USD, micros: 2500000 }
  unit_bytes: 1000000000
  download_only: false
  effective_at: 2026-09-01T00:00:00Z
```

This example means USD 2.50 per decimal GB of upstream upload plus download.
`unit_bytes` accepts decimal GB (`1000000000`) or GiB (`1073741824`). Cost is
rounded half-up once per traffic event and remains explicitly an estimate. A
future-dated rate is stored but does not price traffic before `effective_at`.

Endpoint credentials are references only. For example, an endpoint with
`credential_ref: secret://upstream/auth` reads this JSON value from the process
environment at runtime:

```text
PROXYSIEVE_SECRET_UPSTREAM_AUTH={"username":"demo-user","password":"your-password"}
```

This variable is intentionally not configuration, never appears in effective-config
output, and must never be copied into YAML, CLI args, logs, diagnostics, or commits.
The current environment resolver is local-process only; encrypted persistent secret
storage and rotation remain future work.

An HTTP/HTTPS/SOCKS upstream proxy can resolve the target itself. That means local
validation cannot prove its final target IP. When private-destination restrictions
are enabled, set `trusted_remote_dns: true` on an operator-controlled endpoint only
when that upstream has an explicit destination-enforcement contract. Otherwise the
route is rejected. This intentionally makes unsafe remote DNS configuration fail
closed rather than creating a surprise SSRF path.

Current listener support is HTTP forward/CONNECT and SOCKS5 TCP CONNECT. `auth: local`
is supported only for loopback/trusted use. `auth: password` enables HTTP Basic or
SOCKS5 RFC1929 authentication using the referenced environment credential. The
authenticated username is mapped to an opaque listener-scoped client identity;
credentials are resolved per connection and are never persisted or logged.
SOCKS5 UDP ASSOCIATE is not supported. No route reaches the network until a policy
returns `direct` or `proxy`; `reject`/`block` remain fail-closed.

For an HTTP listener, `auth: api_key` enables downstream `Proxy-Authorization:
Bearer psk_...` authentication against a stored, enabled client key. Create the key
through the local administrator API; only its hash/prefix is retained. Do not place
the raw downstream key in YAML. Password listeners use `Proxy-Authorization: Basic`
for HTTP and username/password sub-negotiation for SOCKS5. API-key auth remains an
HTTP-only mode.
