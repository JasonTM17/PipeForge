---
phase: 3
title: "Local platform end-to-end"
status: completed
priority: P1
effort: "2d"
dependencies: [1, 2]
---

# Phase 3: Local platform end-to-end

## Overview

Make the documented local stack start the complete service graph and prove it
with disposable PostgreSQL, RabbitMQ, and MinIO—not mocks alone.

## Architecture

Default Compose starts API, dispatcher, result consumer, and one active worker
after dependency health gates. Maintenance and monitoring remain explicit
profiles. An e2e harness creates an isolated owner/dataset/job, polls bounded
state transitions, validates artifact content, and captures actionable logs on
timeout.

## File inventory

| Action | Files | Test impact |
|---|---|---|
| Modify | `compose.yaml`, `.env.example`, `Makefile`, bootstrap scripts | startup smoke tests |
| Create | `tests/e2e/` or language-native harness plus fixture data | real-service journey |
| Modify | Dockerfiles/health checks as required | image/startup checks |

## Test scenario matrix

| Priority | Scenario | Proof |
|---|---|---|
| Critical | CSV happy path | terminal success and authorized artifact read |
| Critical | cancel after dispatch | terminal cancellation, no successful projection |
| High | worker crash/lease expiry | bounded retry or documented DLQ/recovery |
| High | service unavailable | diagnostic timeout, no secret leakage |

## Implementation Steps

1. Define service readiness/health checks and remove profiles that hide required
   runtime services from the standard development command.
2. Add PowerShell and Make entrypoints for bootstrap, platform-up, e2e, logs,
   and teardown; never commit `.env` or data artifacts.
3. Create deterministic fixture data and e2e helper clients with unique IDs,
   polling timeouts, and cleanup.
4. Exercise happy, cancellation, retry/failure, and authorization journeys
   against real containers.
5. Verify fresh-clone instructions in a clean temporary environment when Docker
   is available; record any environmental limitation honestly.

## Success Criteria

- [ ] One command starts every service needed to execute a job.
- [ ] E2E proves the full data path without direct database mutation by tests.
- [ ] Failure diagnostics identify service, correlation ID, and last observed
      state without exposing secrets.
