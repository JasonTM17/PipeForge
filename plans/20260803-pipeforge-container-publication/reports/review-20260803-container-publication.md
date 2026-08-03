# Container publication review

Date: 2026-08-03

## Scope

- Role-specific Go and Python Docker targets.
- Compose target selection.
- Dual-registry GitHub Actions publication workflow.
- Registry credential boundary and public pull documentation.
- Version, source SHA, digest, SBOM, provenance, scan, and rollback policy.

## Critical pass

No remaining blocking finding after the scanner and base-image corrections.

- Workflow is not reachable from pull-request code and grants package/OIDC
  permissions only to the publish job.
- Docker Hub credential is consumed only through an Actions secret; GHCR uses
  the job-scoped `GITHUB_TOKEN`.
- Third-party actions are pinned to full commit SHAs.
- Publication rejects malformed versions and non-full source SHAs.
- Existing `0.2.0-local` is not overwritten. The first `0.2.1-local` remote
  publication pushed images but failed while bootstrapping the scanner, so it
  remains failed evidence and the corrected release uses `0.2.2-local`.
- All images run as `pipeforge`; each published role has a fixed entrypoint.
- Trivy 0.73.0 reports zero fixed HIGH or CRITICAL findings for representative
  Go/Alpine and Python/Debian images.

## Informational pass

- `actionlint` 1.7.12: pass.
- Docker build, five targets on `linux/amd64`: pass.
- Entrypoint and non-root metadata, five targets: pass.
- Compose resolved configuration: pass.
- Contract validator: 15 schemas, 14 valid examples, 5 invalid examples.
- Documentation link validation: pass for the changed internal link; existing
  config-key heuristic warnings are unrelated to this change.
- Multi-architecture manifest assembly, remote registry scan, anonymous pull,
  attestation verification, and immutable digest comparison remain release-job
  gates and must be recorded after merge.

## First publication incident

Run `30806374389` built and pushed all five images, then all matrix jobs failed
before scanning because `trivy-action` could not install its default Trivy
0.65.0 binary. Local Trivy scans had already passed. The corrected gate runs
the official Trivy 0.73.0 container pinned to its immutable multi-platform
digest, eliminating the failing installer path while preserving the same
HIGH/CRITICAL policy.

The corrected local scan also identified that Alpine 3.20 had reached end of
support. The Go runtime base is therefore pinned to supported Alpine 3.23.5
before the corrected release.

## Residual limits

- Container publication does not provide cloud HA, backups, SLOs, runtime
  orchestration, or production certification.
- License remains an explicit owner decision.
- The release has no `latest` tag; consumers must use a version or digest.

## Final publication evidence

- Source: `b54db569b303477643e6b5048c2c24e56b5e777f`.
- Release workflow: run `30809299824`, all five publish jobs passed.
- Registries: all five GHCR packages are public and linked to
  `JasonTM17/PipeForge`; anonymous digest resolution passed for both GHCR and
  Docker Hub.
- Platforms: `linux/amd64` and `linux/arm64` present; additional unknown
  manifests are the expected SBOM/provenance attestations.
- GitHub attestation verification: pass for the API package.
- Digest parity: GHCR and Docker Hub match for all five images.
- Follow-up: Docker action pins upgraded to official Node 24 action majors to
  remove the deprecation warning observed in the successful release run.

## Unresolved questions

None for publication. Production deployment and license selection remain
separate owner decisions.
