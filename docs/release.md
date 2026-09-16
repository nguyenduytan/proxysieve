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

Before publishing a draft, verify the hosted CI/security checks and smoke-test at
least one archive on every supported operating system. Do not publish a release
with an incomplete changelog, failed migration test or known P0/P1 defect.

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
