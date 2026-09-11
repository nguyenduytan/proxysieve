# Security policy

## Supported versions

No production-supported version exists yet. The M0 development build does not
implement a network gateway. Do not deploy it for production traffic or secrets.
Support windows will be documented with the first release candidate.

## Report a vulnerability privately

Contact maintainer **Tony Nguyen** at **admin@ndtan.net** with the subject
`[ProxySieve security]`. This is the maintainer contact configured for this project.
Do not create a public issue for an unpatched vulnerability or attach live secrets.
GitHub private vulnerability reporting is a pending repository setup task; do not
assume it is enabled until confirmed.

Include the affected commit/version, OS, deployment mode, sanitized reproduction,
impact, expected behavior, and relevant redacted logs. Use local test servers and
fake credentials. Do not send traffic captures containing other people's data.

We will investigate and coordinate remediation/disclosure; no response-time SLA
is promised for this early-stage community project.
