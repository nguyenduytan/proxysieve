# Operations

ProxySieve provides bounded, local-only workflows for database maintenance,
recovery and moving configuration between installations. Commands validate the
effective configuration and never print raw secrets.

## Database Status and Maintenance

Stop ProxySieve before migration or compaction. Status may be queried for human or
machine-readable output:

```sh
proxysieve db status [--file CONFIG.yaml]
proxysieve db status [--file CONFIG.yaml] --json
proxysieve db migrate [--file CONFIG.yaml]
proxysieve db compact [--file CONFIG.yaml]
```

Status reports the schema version, journal mode and inventory row counts. Migration
opens the configured SQLite database and applies the embedded, checksum-verified
migration history. Compaction runs SQLite `VACUUM` to reclaim unused pages.

## Backup and Restore

Stop ProxySieve before backup or restore:

```sh
proxysieve backup --file CONFIG.yaml --path proxysieve-backup.db
proxysieve restore --file CONFIG.yaml --path proxysieve-backup.db
```

Backup creates a WAL-consistent SQLite snapshot and refuses to overwrite a file.
Restore validates the embedded migration history before replacing anything, then
archives the previous database and WAL/SHM sidecars with a timestamp. The database
snapshot preserves routing inventory, active runtime revisions and analytics state.

For a fresh installation, restore the database and import the portable config below.
The config archive reproduces settings and secret references, while operators must
provision the referenced secret values separately.

## Portable Export

```sh
proxysieve export [--file CONFIG.yaml] --path proxysieve.export.zip
```

The archive is versioned and contains a manifest plus `config.json`. It includes
proxy, pool, policy, listener, budget and source metadata, including credential
references, but never reads or copies the referenced secret values. The archive is
limited to 8 MiB and is written with restrictive permissions.

## Import and Validation

Validate without writing a file:

```sh
proxysieve import --path proxysieve.export.zip --dry-run
```

Write a new YAML configuration after validation:

```sh
proxysieve import --path proxysieve.export.zip --output config.imported.yaml
```

Import is intentionally a file-generation boundary. It does not activate a live
runtime, modify SQLite, or resolve credentials. Review the generated configuration
and activate it through the normal startup/control-plane workflow. Use backup and
restore for database state; stop the running process before either database
operation.
