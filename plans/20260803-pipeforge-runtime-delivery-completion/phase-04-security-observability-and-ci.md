---
phase: 4
title: "Security observability and CI"
status: completed
priority: P1
effort: "2d"
dependencies: [3]
---

# Phase 4: Security observability and CI

## Overview

Protect and continuously verify the runnable platform. This consolidates the
essential portions of original Phases 18–20 before anything is called a release.

## File inventory

| Action | Files | Test impact |
|---|---|---|
| Create | `.github/workflows/*`, security policy helpers/tests, runbooks | CI matrix/security regression |
| Modify | API upload/job boundaries, logging/metrics, Compose/Dockerfiles | quota/redaction/health tests |
| Create | dependency/secret scanning configuration | supply-chain gates |

## Requirements

- Enforce request/upload/job/storage bounds, timeouts, owner isolation, and
  redaction at server boundaries; use a documented local-only limiter.
- Emit correlation fields across API, outbox, dispatcher, worker, and results;
  labels must remain bounded.
- CI validates formatting, type/build/race, contracts, migrations, image build,
  dependency/secret scan, and Compose-backed integration/e2e without cloud keys.

## Implementation Steps

1. Implement only the missing controls proven by an updated threat model; avoid
   speculative policy abstractions.
2. Add trace/correlation propagation and operational metrics required to debug
   a single job across services.
3. Add CI workflows with fast checks and a containerized integration job;
   protect release workflow from untrusted secrets.
4. Add redaction, quota, authorization, retry/DLQ, and observability assertions.
5. Verify Docker image user/health behavior and document non-production limits.

## Success Criteria

- [ ] Every job can be followed by correlation ID across all local services.
- [ ] Resource limits and authorization failures have deterministic API tests.
- [x] CI is valid, offline-safe where possible, and requires no real secrets.
- [x] No critical secret or image-user issue remains unresolved in the local
  learning scope; dependency freshness remains a release-owner gate.
