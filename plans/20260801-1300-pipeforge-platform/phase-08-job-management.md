---
phase: 8
title: Job management
status: completed
priority: P1
effort: 5d
dependencies:
  - 4
  - 5
  - 7
---

# Phase 8: Job management

## Overview

Implement processing jobs, attempts, controlled lifecycle transitions, idempotent job creation, transactional outbox requests, and a simple fair scheduler with per-user admission limits.

## Requirements

- States and transitions are explicit; no unrestricted status-update endpoint.
- Job creation validates owner, dataset availability, operation contracts, fingerprint, and idempotency key.
- A single transaction creates the job, initial attempt/state history, idempotency record, and outbox message.
- Scheduler is fair and bounded; it does not claim work without a worker/lease contract.

## Architecture

`job` owns state-machine rules and history. `scheduler` selects queued jobs using `FOR UPDATE SKIP LOCKED`, per-user quotas, priority, and age to avoid starvation. The initial scheduler publishes only through outbox; lease assignment is completed in the lease phase. API resources expose safe projections and pagination.

## Related Code Files

- Create: `go/internal/job/`, `go/internal/scheduler/`
- Create: `go/migrations/000007_jobs.sql`
- Modify: `go/internal/outbox/`, dataset-version job handlers, OpenAPI
- Create: job state-machine, authorization, idempotency, fairness tests
- Modify: contracts job-request schema and docs

## Implementation Steps

1. Add job, operation, attempt, transition-history, idempotency, and quota counters/schema indexes.
2. Define transition table/functions with invalid-transition errors and terminal-state protections.
3. Validate operation types/configs and compute canonical request fingerprint.
4. Implement create/list/get/cancel-intent/retry-intent endpoints with resource ownership and idempotency replay semantics.
5. Implement scheduler selection/admission with bounded batch, fairness, and backpressure metrics.
6. Publish `processing.job.requested` via outbox and add tests for transaction rollback/retry/duplicate requests.

## Success Criteria

- [x] Valid job request creates one stable resource for repeated idempotency key/fingerprint.
- [x] Conflicting idempotency reuse returns a deterministic conflict.
- [x] Invalid transitions and terminal updates are rejected without partial state changes.
- [x] Scheduler does not starve an older eligible user behind a noisy user within the documented fairness model.
- [x] Job API never exposes stack traces or arbitrary state mutation.

## Validation

- Go state-machine property/table tests
- PostgreSQL transaction tests for idempotency/outbox atomicity
- scheduler fairness test with multiple users and queue backlog
- HTTP authorization/pagination tests

## Risk Assessment

- Risk: request fingerprint depends on JSON map ordering. Mitigation: canonical JSON normalization before hashing.
- Risk: scheduler creates duplicate commands. Mitigation: state/claim locks and outbox idempotency keys; worker/result layers remain at-least-once safe.

## Security Considerations

Validate operation config against allowlisted schemas, cap operation count and request size, enforce dataset ownership, and redact configs if they may contain sensitive values.

## Next Steps

Phase 9 implements the typed Python worker that consumes the job request contract.

## Unresolved Questions

- None blocking; initial fairness uses weighted round-robin by owner plus age, documented and tested before optimizing.
