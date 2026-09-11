# Contributing to ProxySieve

Welcome! The project is created and maintained by **Tony Nguyen**.

Read [PLAN.md](PLAN.md), [PROGRESS.md](PROGRESS.md), and
[development instructions](docs/development.md) before starting work. Check for
existing code and issues instead of introducing competing implementations.

Use a short-lived branch (`feat/`, `fix/`, `docs/`, or `codex/` for agent work),
Conventional Commits, and a focused PR. Describe security, traffic-accounting,
compatibility, test, benchmark, and documentation impact. `main` must be releasable;
the initial bootstrap honestly advertises only the capabilities it implements.

Public reusable contracts go in `pkg/` when a milestone needs them. Wiring and
adapters go in `internal/`. Public interfaces must not expose SQLite, UI, provider,
or third-party implementation types. New providers, selectors, policy actions,
SQL migrations, and OpenAPI updates arrive with their owning milestones; until
then there is no public extension/API compatibility promise.

Run Go formatting, vet, tests, and frontend lint/test/build before a PR. Run the
race detector on a supported environment; CI uses Linux. Hot-path changes need
benchmarks. Network tests must use deterministic local servers, not paid proxies.
Do not weaken tests or safety checks to make a build green.

Never commit real credentials, private keys, proxy lists with live auth, or traffic
captures. Use `example.invalid` and explicitly fake credentials in tests. Report
vulnerabilities using [SECURITY.md](SECURITY.md), not public issues.

Contributions are accepted under the repository's Apache-2.0 license. Preserve
existing notices and recognize contributors. Follow the [code of conduct](CODE_OF_CONDUCT.md).
