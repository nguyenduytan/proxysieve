# Operations

ProxySieve provides two bounded, local-only workflows for moving configuration
between installations. Both commands validate the effective configuration before
writing and refuse to overwrite an existing destination.

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
