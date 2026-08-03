---
title: PipeForge container publication
description: Publish verified multi-architecture runtime images to GHCR and Docker Hub.
status: completed
priority: P1
branch: ci/container-publication
tags:
  - release
  - containers
  - security
created: '2026-08-03'
createdBy: 'ck:plan'
---

# PipeForge container publication

## Objective

Turn the locally verified release into pullable, role-specific container
images on GHCR and Docker Hub without overstating production readiness.

## Acceptance criteria

- Five non-root images publish to both registries with matching version and
  full-source-SHA tags.
- Published manifests support `linux/amd64` and `linux/arm64`.
- The release gate rejects fixed HIGH or CRITICAL vulnerabilities.
- Each image carries OCI source/revision/version labels, BuildKit SBOM and
  provenance, a GitHub artifact attestation, and retained digest evidence.
- Compose still resolves and every role-specific image has the intended
  entrypoint.
- Pull instructions and remaining production limitations match reality.

## Phases

| Phase | Scope | Status |
|---|---|---|
| 1 | Registry and image audit | Completed |
| 2 | Role-specific targets and publication workflow | Completed |
| 3 | Local validation and adversarial review | Completed |
| 4 | PR, merge, dual-registry publication, evidence | Completed |

## Constraints

- Do not choose a license for the owner.
- Do not publish `latest` for the prerelease.
- Do not print or commit registry credentials.
- Do not call this a production-certified deployment.

## Rollback

Disable the workflow and consume a prior immutable digest. Do not overwrite an
existing version tag; publish a corrected version and retain the failed run as
audit evidence.
