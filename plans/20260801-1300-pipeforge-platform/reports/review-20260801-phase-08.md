# Phase 8 review report: job management

## Scope

- Reviewed the Phase 8 diff from `d4bc257` through the current implementation and contract hardening.
- Focused on transaction atomicity, concurrency, authorization, state transitions, input validation, public error redaction, scheduler fairness, and contract alignment.
- No parallel reviewer agent was available because the configured agent thread limit was already occupied; review was performed against the project review checklist with runnable evidence.

## Overall assessment

PASS for the Phase 8 scope. The implementation keeps PostgreSQL authoritative, uses row locks for lifecycle commands and bounded `SKIP LOCKED` selection, scopes idempotency by owner, and keeps RabbitMQ outside the database transaction through the outbox.

## Critical issues

None found.

## High priority issues

None found.

## Medium priority / deferred boundaries

- `SelectQueued` intentionally returns advisory candidates and does not assign leases. This is documented and is required to avoid inventing a worker contract before the lease/result phases.
- Result acceptance, lease fencing, heartbeat renewal, timeout recovery, and dead-letter consumption are not implemented in this phase; they are explicit later-phase dependencies rather than missing Phase 8 behavior.

## Edge cases verified

- Concurrent idempotency insertion is handled with a unique constraint and conflict replay.
- Outbox insertion failure rolls back job, attempt, history, quota, and idempotency mutations.
- Owner mismatch is rejected for both reads and lifecycle commands; admin cross-owner listing/commands are explicit.
- Cancel decrements queued quota only for queued jobs; retry admission is quota-checked transactionally.
- Public job JSON omits internal last-error text, and handlers map repository/dataset not-found errors without leaking internals.
- Operation schemas reject unsupported types, unknown config keys, invalid column identifiers, empty column lists, and unsupported anomaly methods.

## Verification metrics

- Unit tests: pass.
- Vet: pass.
- Race tests for job/scheduler/httpapi: pass.
- PostgreSQL job integration: pass.
- Contract fixtures: 9 valid / 4 invalid pass.
- Documentation validator: pass.
- Static coverage threshold: not configured by the repository.

## Recommended actions

1. Carry the job state/attempt identifiers into the lease and result-consumer phases.
2. Keep the shared JSON Schema and Go validation tests updated together when adding operations.
3. Add worker-side contract fixtures before enabling command consumption.

## Unresolved questions

- The exact lease duration and heartbeat cadence belong to the worker/result design and remain intentionally open until Phase 9–16 integration.
