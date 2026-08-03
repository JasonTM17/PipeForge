# PipeForge local learning release

Version: `0.2.3-local`

This folder is the release evidence for the verified local runtime. It is not
a production certification and does not claim cloud availability, autoscaling,
or multi-region scheduling.

## Included evidence

- [validation-evidence.md](./validation-evidence.md) — commands and outcomes.
- [known-limitations.md](./known-limitations.md) — deliberate non-goals and
  operational caveats.
- [release-notes.md](./release-notes.md) — user-visible release summary.
- [container-0.2.3-evidence.md](./container-0.2.3-evidence.md) — current
  dual-registry tags, digests, and workflow evidence.
- [local-e2e-runbook](../operations/local-e2e-runbook.md) — reproducible demo.
- [system architecture](../assets/images/system-architecture.png) and
  [job lifecycle](../assets/images/job-lifecycle.png) — rendered visuals.
- [local E2E flow GIF](../assets/videos/local-e2e-flow.gif) — compact evidence
  storyboard generated from the successful `make e2e` flow.

## Version policy

The project follows semantic-version-shaped tags. `0.x` means the public API
and contracts may still evolve. A release candidate must include passing CI,
contract validation, a clean review, and a fresh Compose E2E run. A production
release requires a reviewed deployment design, backup/restore test, SLOs,
and an external security assessment. The repository threat model documents
current controls and residual risks; it is not a penetration-test result.

## Rollback

For this local package, rollback means returning to the previous Git commit and
recreating the Compose images. Never roll back migrations by deleting rows.
Database migration rollback requires a reviewed forward migration or a tested
backup restore procedure; neither is claimed by this learning release.
