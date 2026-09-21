# Release process

ProxySieve releases are built only from semantic-version tags such as
`v1.0.0-rc.1` or `v1.0.0`. A tag starts the pinned release workflow, which runs
the backend verification gates and creates a **draft** GitHub Release. Creating a
tag does not publish the draft.

The draft contains Linux, Windows and macOS archives for amd64 and arm64, a
`checksums.txt` file and `proxysieve.spdx.json`. Each archive also includes the
example configuration, operator documentation and runtime-dependency-free browser adapter
sources. GitHub artifact attestations
bind the archives and SBOM to the tag workflow. Pre-release tags remain
pre-releases.

The tag workflow checksum-verifies, extracts and executes the amd64 archive on
Linux, Windows and macOS before it can be considered green. Before publishing a
draft, also verify the hosted CI/security checks and review those native smoke
results. Do not publish a release with an incomplete changelog, failed migration
test or known P0/P1 defect.

Verify an extracted download against the release checksum:

```sh
sha256sum --check checksums.txt --ignore-missing
```

PowerShell users can compare the matching `checksums.txt` digest with:

```powershell
Get-FileHash .\proxysieve_1.0.0_windows_amd64.zip -Algorithm SHA256
```

Verify GitHub provenance after downloading an archive or SBOM:

```sh
gh attestation verify proxysieve_1.0.0_linux_amd64.tar.gz --repo nguyenduytan/proxysieve
gh attestation verify proxysieve.spdx.json --repo nguyenduytan/proxysieve
```

The workflow intentionally does not publish a container image or move a
pre-release to `latest`. Container publication remains gated on the full release
acceptance matrix.

## Compatibility matrix

| Surface | Supported release contract |
| --- | --- |
| Runtime targets | Linux, Windows and macOS on amd64 and arm64; release archives are statically built with Go 1.27.x (`CGO_ENABLED=0`) |
| Downstream protocols | HTTP/1.1 forward proxy, HTTP CONNECT and SOCKS5 CONNECT; no SOCKS5 UDP |
| Upstream routes | Explicit DIRECT, HTTP proxy, HTTPS proxy and SOCKS5 proxy, including ordered 2-to-8-hop chains |
| HTTPS Inspect | Explicitly scoped HTTP/1.1 inside CONNECT only; disabled by default; no HTTP/2, HTTP/3, QUIC, WebSocket upgrade or nested CONNECT inspection |
| Browser adapters | Node.js 24; Playwright Core 1.63.x; Puppeteer Core 25.11.x; Selenium uses the documented proxy-only setup |
| Versioned contracts | Configuration schema v1, Admin API v1 at `/api/v1`, browser control/report format v1, portable export format v1 and Extension API v1 |
| SQLite backup/restore | Stop ProxySieve first. Backups include committed WAL state, never overwrite a destination and may restore a valid older embedded schema that the current binary can migrate. Future, reordered or checksum-modified migration histories are rejected. |

Static cross-compilation is not runtime acceptance. Before publishing an RC, the
hosted Linux, Windows and macOS protocol jobs and native amd64 archive smoke jobs
must pass. Arm64 artifacts remain build-verified until matching hardware or hosted
runners execute them.
