# Deployment

Release archives contain one static binary, the example configuration, operator
docs and browser adapter sources. Verify the archive checksum and provenance as
described in [release.md](release.md), then copy `config.example.yaml` and review
every bind, route, secret reference and retention value before startup.

ProxySieve defaults to loopback Admin and gateway listeners. Keep Admin on
loopback for the first release. An unauthenticated gateway may not bind publicly;
remote Admin TLS is not yet a supported deployment path. DIRECT, private-network
access and HTTPS Inspect remain explicit opt-ins.

The process needs write access to its configured data directory for SQLite,
imports, optional disk cache and Inspect CA. Run one process per data directory.
Stop it before migration, compaction, backup or restore. Provision referenced
secrets through the documented environment boundary rather than baking them into
images or configuration.

The supplied container is a non-root, read-only artifact smoke target. It exposes
no port and starts with `version`; a real container deployment must explicitly
mount a writable data directory and reviewed config and publish only intended
listeners. No container image is published until the release gate says so.

After deployment run `proxysieve doctor`, complete the browser-only first-run
flow, send one controlled request, and verify its policy, pool, proxy and byte
attribution in Admin. Back up the database before upgrades. See
[operations](operations.md) and [troubleshooting](troubleshooting.md).

