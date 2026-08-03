# Container release evidence — 0.2.2-local

Captured on 2026-08-03 for source
`b54db569b303477643e6b5048c2c24e56b5e777f`.

## Publication result

- [GitHub prerelease](https://github.com/JasonTM17/PipeForge/releases/tag/v0.2.2-local)
- [Container release workflow](https://github.com/JasonTM17/PipeForge/actions/runs/30809299824): prepare and all five publish jobs passed
- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `0.2.2-local` and
  `sha-b54db569b303477643e6b5048c2c24e56b5e777f`
- GHCR visibility: public; all packages linked to `JasonTM17/PipeForge`
- Anonymous manifest resolution: passed for all ten GHCR and Docker Hub tags
- Trivy 0.73.0: zero fixed HIGH or CRITICAL findings for every image
- Runtime check: every image runs as `pipeforge` with its role-specific
  entrypoint
- Supply chain: BuildKit maximum-mode provenance and SBOM plus GitHub artifact
  attestation

## Immutable digests

| Image | Digest |
|---|---|
| `pipeforge-api` | `sha256:71a6a63e018a7b46ece95751cd53fd7ac7d6e86b8dd8b1ee21865145bef38c02` |
| `pipeforge-migrate` | `sha256:ece2a1e06aded26f91fe91aa3d8f3859806579df7c8522511f5098a8ded3413c` |
| `pipeforge-result-consumer` | `sha256:f80acc3f870cd3d56c0e1853d77449d2948da7a1e7f3ee7222878eb3b03fa020` |
| `pipeforge-scheduler` | `sha256:9c6ffd89f48ea1d6beb7444980a62f46f41f502e033440baed13b4350015e024` |
| `pipeforge-worker` | `sha256:018738dd155b3459a58c05e9e8d86984b8517a7467d073b892244175274eea90` |

Each digest is identical between `ghcr.io/jasontm17/<image>` and
`nguyenson1710/<image>`.

## Verification performed

- Downloaded all five `container-digest-*` workflow artifacts and matched
  version, full source SHA, and registry references.
- Resolved both registry tags with an empty Docker configuration to prove
  anonymous access and digest parity.
- Confirmed API manifest includes both required Linux architectures.
- Verified the API OCI attestation against `JasonTM17/PipeForge` with GitHub
  CLI.

The earlier `0.2.1-local` run is retained and marked as a failed publication:
images were pushed, but its scanner bootstrap failed before the release gate
could complete. Consumers must use `0.2.2-local` or the digests above.

This evidence proves container publication, not production deployment. Cloud
HA, backups, SLOs, penetration testing, operational support, and license
selection remain outside this learning release.
