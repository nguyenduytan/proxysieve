# CLI

Run `proxysieve help` for the authoritative command syntax. Commands return `0`
on success, `1` for an operational failure and `2` for invalid usage. Arbitrary
arguments are not echoed on usage errors because future arguments may be secret.

The main command groups are:

- `start`, `doctor`, `version` for runtime startup and diagnostics;
- `config validate|print-effective` for schema-v1 configuration;
- `db status|migrate|compact`, `backup`, `restore`, `export`, `import` for local
  operations (stop the running process before write maintenance);
- `session list|show|rotate|delete` and `cache stats|purge|purge-domain` for
  authenticated Admin operations;
- `inspect ca init|fingerprint|export|rotate` for the optional local Inspect CA;
- `extension call` for an explicitly installed, trusted local extension.

Admin commands read the `PSV_ADMIN_PASSWORD` environment variable and
default to the loopback Admin URL. Do not put passwords, API keys, proxy
credentials or setup tokens in command arguments, YAML or shell history. Use
`--json` where documented for automation.

See [configuration](configuration.md), [operations](operations.md),
[HTTPS Inspect](https-inspect.md) and [extensions](extensions.md) for behavior and
safety constraints.
