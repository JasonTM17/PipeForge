# Container publication review

Date: 2026-08-03

## Scope

- Role-specific Go and Python Docker targets.
- Compose target selection.
- Dual-registry GitHub Actions publication workflow.
- Registry credential boundary and public pull documentation.
- Version, source SHA, digest, SBOM, provenance, scan, and rollback policy.

## Critical pass

No blocking finding.

- Workflow is not reachable from pull-request code and grants package/OIDC
  permissions only to the publish job.
- Docker Hub credential is consumed only through an Actions secret; GHCR uses
  the job-scoped `GITHUB_TOKEN`.
- Third-party actions are pinned to full commit SHAs.
- Publication rejects malformed versions and non-full source SHAs.
- Existing `0.2.0-local` is not overwritten; new images use `0.2.1-local`.
- All images run as `pipeforge`; each published role has a fixed entrypoint.
- Trivy 0.72.0 reports zero fixed HIGH or CRITICAL findings for representative
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

## Residual limits

- Container publication does not provide cloud HA, backups, SLOs, runtime
  orchestration, or production certification.
- License remains an explicit owner decision.
- The release has no `latest` tag; consumers must use a version or digest.

## Unresolved questions

None for publication. Production deployment and license selection remain
separate owner decisions.
