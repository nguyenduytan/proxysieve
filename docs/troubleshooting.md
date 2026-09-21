# Troubleshooting

Start with:

```sh
proxysieve doctor --file config.yaml
proxysieve config validate --file config.yaml
proxysieve db status --file config.yaml --json
```

Common failures:

- `DESTINATION_DENIED`: the target is private/unsafe, DIRECT is not explicitly
  allowed, or the selected remote-DNS proxy has not been explicitly trusted.
- `POLICY_BLOCKED` / `ROUTE_UNAVAILABLE`: simulate the request in Policies, then
  verify that the policy is active and its pool has an enabled, healthy endpoint.
- Proxy health succeeds but routing is denied: health proves connectivity, not an
  upstream provider's destination enforcement. Review the endpoint and approve
  remote DNS only when that trust is justified.
- `407` or proxy authentication failure: verify the endpoint's secret reference
  and process environment. Never paste its raw value into logs or issues.
- Admin mutation returns `401`/`403`: sign in again, check the user's role and use
  the CSRF value issued for that session. Operators cannot perform admin-only
  security or credential lifecycle actions.
- Inspect reports a missing CA: run `proxysieve inspect ca init`, export only the
  public certificate, trust it in the controlled client, then restart. Do not
  distribute `data/inspect/ca.pem`; it contains the private key.
- Saved edits do not affect traffic: activate the complete inventory from Policies.
  Activation is deliberately separate and atomic.
- Database maintenance refuses to run: stop ProxySieve first and confirm no other
  process owns the configured data directory.

Use the sanitized Events, Audit, Health and Traffic workspaces for evidence. Do
not attach live proxy credentials, raw API keys, setup tokens, cookies, private CA
bundles or captured user traffic to a bug report. Security reports follow
[SECURITY.md](../SECURITY.md).

