---
phase: 1
title: Lease-bound dispatch
status: completed
priority: P1
effort: 2d
dependencies: []
---

# Phase 1: Lease-bound dispatch

## Overview

Turn the existing transactional job/outbox record into a lease-bound worker
command without moving authority out of PostgreSQL. A queued job may be
dispatched once per valid lease and every redelivery must remain safe.

## Architecture

`job.create` persists intent and a `processing.job.queued` outbox signal. The
scheduler consumes that durable signal (with periodic database reconciliation),
selects eligible work fairly, and atomically acquires a lease plus a second
outbox record for the fenced `processing.job.requested` command. The generic
outbox dispatcher publishes that execution command only after commit. The local
reference topology uses one configured worker UUID shared by scheduler and
worker; worker registration/multi-worker assignment is explicitly deferred.
RabbitMQ is transport only; it cannot create or extend leases.

## File inventory

| Action | Files | Test impact |
|---|---|---|
| Modify | `go/cmd/scheduler/main.go`, `go/internal/scheduler/*`, `go/internal/lease/*`, `go/internal/queue/*` | dispatch lifecycle/race tests |
| Modify | `contracts/json-schema/processing.job.requested.v1.schema.json`, examples, docs messaging | schema compatibility tests |
| Create | dispatcher runtime/service tests | contention, publish, shutdown tests |

## Interface checklist

- `processing.job.queued` is a non-executable scheduler signal; only the lease
  transaction creates `processing.job.requested`.
- `processing.job.requested` is fence-complete with `attemptId`, `leaseId`,
  `workerId`, `attemptNumber`, operations, and immutable source metadata.
- Command source metadata is server-resolved and limited to immutable object key,
  content type, format, and size—never a user-controlled filename, credentials,
  or raw data.
- Publisher confirms precede outbox acknowledgement; failed publication remains
  retryable and must not consume a job lease silently.

## Test scenario matrix

| Priority | Scenario | Proof |
|---|---|---|
| Critical | two dispatcher ticks target one job | one lease/command only |
| Critical | publish fails after lease acquire | outbox retry or safe recovery |
| High | lease expires before result | stale result rejected |
| High | dispatcher shutdown | context cancellation and no leaked loop |

## Implementation Steps

1. Write failing service tests for queued-signal materialization, eligible-job
   selection, leased command materialization, duplicate delivery, and publish
   failure.
2. Add the dispatcher loop with bounded polling/backoff and graceful shutdown;
   reuse existing fair-selection, lease, outbox, and queue abstractions.
3. Version and validate the complete command payload plus positive/negative
   fixtures; update the control-plane message producer only after tests pass.
4. Start the scheduler command as a continuous queue consumer, database
   reconciler, outbox dispatcher, cleanup, and lease-sweep loop with bounded
   intervals and graceful shutdown.
5. Run Go unit/race/integration tests and contract validation.

## Success Criteria

- [ ] Every executable command has non-null attempt and lease identities.
- [ ] A duplicate delivery cannot create a second valid lease.
- [ ] Publisher failure is observable and recoverable without a lost job.
- [ ] Dispatcher stops within the configured shutdown deadline.

## Risks

Acquiring a lease before broker publication creates a failure window. Mitigate
through durable outbox state, confirm-before-ack, bounded retry, and expiry
recovery rather than unbounded requeueing.
