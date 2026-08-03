# Container release evidence — 0.2.3-local

Captured on 2026-08-03 for source
`bb98949a0651eaedbd59b603626dbc233771be21` after PR 16 merged.

## Publication result

- [Container release workflow](https://github.com/JasonTM17/PipeForge/actions/runs/30816637498): metadata and all five publish jobs passed
- Platforms: `linux/amd64`, `linux/arm64`
- Tags: `0.2.3-local` and
  `sha-bb98949a0651eaedbd59b603626dbc233771be21`
- Registries: public GHCR packages linked to `JasonTM17/PipeForge` and matching
  public Docker Hub repositories under `nguyenson1710`
- Security: every image passed the fixed HIGH/CRITICAL Trivy gate
- Runtime: every image was pulled by digest and verified as non-root user
  `pipeforge` with its role-specific entrypoint
- Supply chain: maximum-mode provenance, SBOM, GitHub artifact attestation, and
  90-day digest evidence

## Immutable digest parity

| Image | GHCR and Docker Hub digest |
|---|---|
| `pipeforge-api` | `sha256:adf7da821cde94cb0fb0f8dc64ec66e80fef335cdcec144d5c5b4226931a2dfa` |
| `pipeforge-scheduler` | `sha256:eead811258c853516991f4df7c53ccdff9a94fdcfb5d4e9f372ff89fc256bf7f` |
| `pipeforge-result-consumer` | `sha256:df3ffb4ae07b158b497a6737936f603bdc1bf9c97765ffb1e5f587a061b3eb74` |
| `pipeforge-migrate` | `sha256:9b87180d5070d3df8734f618e2302a4404cca3107d6a8d563d976e0c966feb62` |
| `pipeforge-worker` | `sha256:5dd4360910c11b8d3c15723443f5eebc6661fd7098858ef01c2178657623ca77` |

Digest parity was resolved directly from both registry manifests after the
workflow completed. Consumers should pin these immutable digests for
repeatable evaluation.

This publication proves reviewed build and registry delivery, not production
deployment. Cloud HA, backup/restore, SLOs, external penetration testing,
operational support, and owner-selected licensing remain outside this learning
release.
