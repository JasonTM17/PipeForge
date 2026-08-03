---
title: PipeForge professional hardening
description: >-
  Close verified API, console, CI, security, and documentation gaps before the
  next PipeForge learning release.
status: completed
priority: P1
branch: improvement/professional-hardening
tags:
  - bugfix
  - frontend
  - backend
  - auth
  - infra
  - docs
  - critical
blockedBy: []
blocks:
  - 20260801-1300-pipeforge-platform
  - 20260803-pipeforge-runtime-delivery-completion
created: '2026-08-03T12:18:11.016Z'
createdBy: 'ck:plan'
source: skill
---

# PipeForge professional hardening

## Overview

Implement every actionable finding from the 2026-08-03 full-repository audit.
Preserve all public HTTP and message contracts while making the local learning
release deterministic, accessible, security-triaged, and continuously verified.

## Scope

- Add bounded API server timeouts and abuse protection for public auth routes.
- Remove console request races, stale cross-user state, and inaccessible async states.
- Add deterministic unit/component/browser tests and full Compose release gates.
- Reconcile plans, security docs, release evidence, and tracked binary policy.
- Keep cloud HA, external deployment, and license selection outside implementation;
  license remains an explicit owner decision.

## Acceptance criteria

- Concurrent console requests cannot display artifacts for the wrong job or user.
- Console login, 401 recovery, loading/error/empty states, keyboard access, and
  375px/1440px layouts have repeatable automated coverage.
- API rejects abusive auth bursts, bounds request/server lifetimes, and keeps
  existing response bodies and routes compatible.
- CI runs language checks, frontend unit/browser tests, and real single/two-worker
  Compose E2E with cleanup and uploaded evidence.
- CodeQL alert 1 is adjudicated with source evidence and no unresolved High alert.
- Docs and plan statuses describe the actual implementation and validation source.
- The final worktree passes full local gates and an adversarial pending-diff review.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [API security and resilience](./phase-01-api-security-and-resilience.md) | Completed |
| 2 | [Console correctness and accessibility](./phase-02-console-correctness-and-accessibility.md) | Completed |
| 3 | [Automated quality and release gates](./phase-03-automated-quality-and-release-gates.md) | Completed |
| 4 | [Documentation and repository completion](./phase-04-documentation-and-repository-completion.md) | Completed |

## Dependencies

- Extends the completed multi-worker and container publication plans.
- Blocks truthful completion of the original platform and runtime-delivery trackers.
- Requires no public API or message schema migration.
