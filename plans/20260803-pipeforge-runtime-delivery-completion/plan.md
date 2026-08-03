---
title: PipeForge runtime delivery completion
description: >-
  Turn the existing control-plane and data-plane slices into a verified local
  end-to-end learning product with an auditable release package.
status: in-progress
priority: P1
branch: main
tags:
  - feature
  - backend
  - infra
  - docs
  - critical
blockedBy: []
blocks: []
created: '2026-08-03T01:00:44.034Z'
createdBy: 'ck:plan'
source: skill
---

# PipeForge runtime delivery completion

## Overview

Complete the missing runtime links identified after Phase 16: a Go dispatcher
must acquire a lease and emit a fenced command; a Python worker must process
the object and publish attempt-scoped artifacts/events; the Go result consumer
must project the outcome. The completed repository is a local, reproducible
learning product—not a production cloud deployment.

## Scope and non-goals

- In scope: one real CSV happy path, cancellation/failure/retry coverage, Compose
  startup, disposable-service integration/e2e tests, CI, release metadata,
  `pipectl`, the approved operator console, security/observability baseline,
  docs/runbooks, architecture diagrams, and a GIF recorded from the real local
  demonstration flow.
- Out of scope: cloud hosting, Kubernetes, multi-region HA, paid services,
  production credentials, and inventing a license without an owner decision.
- Contract rule: PostgreSQL stays authoritative. Workers receive only a fenced,
  server-resolved command and never read or write control-plane tables.

## Acceptance evidence

1. A fresh clone follows documented PowerShell and Make alternatives to start
   PostgreSQL, RabbitMQ, MinIO, API, dispatcher, worker, and result consumer.
2. An e2e fixture performs upload → job → outbox publish → lease-bound command
   → worker processing/artifacts → result projection → authorized artifact
   download; cancellation and a retryable failure have deterministic assertions.
3. CI executes Go, Python, contract, migration, Compose, integration/e2e,
   secret/dependency/image checks without cloud credentials.
4. The release folder contains version metadata, changelog, validation evidence,
   known limits, rollback notes, diagrams, screenshots, and an actual GIF.

## Execution order

1. Phase 1 establishes the control-plane dispatch contract.
2. Phase 2 binds the Python runtime to that contract and artifacts.
3. Phase 3 proves the full path against real services.
4. Phase 4 closes delivery gates that protect the runtime path.
5. Phase 5 makes the demonstrable product operable through CLI and console.
6. Phase 6 records verified behavior in documentation and release assets.

## Phases

| Phase | Name | Status |
|-------|------|--------|
| 1 | [Lease-bound dispatch](./phase-01-lease-bound-dispatch.md) | Completed |
| 2 | [Worker execution and artifacts](./phase-02-worker-execution-and-artifacts.md) | Completed |
| 3 | [Local platform end-to-end](./phase-03-local-platform-end-to-end.md) | Completed |
| 4 | [Security observability and CI](./phase-04-security-observability-and-ci.md) | Completed |
| 5 | [Developer experience and operator console](./phase-05-developer-experience-and-operator-console.md) | Completed |
| 6 | [Documentation assets and release package](./phase-06-documentation-assets-and-release-package.md) | Completed |

## Dependencies

- Builds on `plans/20260801-1300-pipeforge-platform/` Phases 1–16.
- This plan supplies the missing execution evidence required before that plan's
  Phase 20 delivery gate can honestly be marked complete.
- Phase 5 consumes stable API/runtime behavior from Phases 1–4; Phase 6 is the
  final documentation and release gate, never a substitute for testing.
