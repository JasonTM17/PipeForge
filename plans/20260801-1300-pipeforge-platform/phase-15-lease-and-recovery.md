---
phase: 15
title: "Lease and recovery"
status: pending
priority: P1
effort: "5d"
dependencies: [8, 14]
---

# Phase 15: Lease and recovery

## Overview

Implement worker/job leases, renewal, expiration detection, bounded exponential retry with jitter, dead-letter persistence, safe replay, and crash-recovery tests.

## Requirements

- Lease fields include job, attempt, lease, worker, leased/expiry/renewal timestamps, and attempt number.
- Progress/result events carry lease and attempt identity; stale results are ignored.
- Retry classification distinguishes temporary infrastructure/worker failures from permanent input/config/authorization failures.
- Maximum attempts and backoff are bounded; terminal dead-letter state is inspectable/replayable with authorization/audit.

## Architecture

Go assigns/renews leases transactionally. Workers renew on a timer and stop work when renewal fails or cancellation is requested. A scheduler sweeps expired leases with row locks, marks attempt timeout, creates the next attempt if policy allows, and lets outbox publish the retry. DLQ records preserve safe diagnostics, not raw data/stack traces. Replay creates a controlled new attempt/fingerprint with audit.

## Related Code Files

- Create: `go/internal/lease/`, `go/internal/retry/`, `go/internal/dlq/`
- Create: `go/migrations/000010_leases_retries_dlq.sql`
- Modify: scheduler, result consumer, job transitions, contracts, API/admin handlers
- Create: lease/retry/DLQ unit/integration/e2e tests
- Create/modify: `docs/architecture/worker-failure-sequence.md`, retry/DLQ docs/runbook

## Implementation Steps

1. Add lease/renewal/attempt timeout schema and state-machine invariants.
2. Implement lease acquisition/renewal with owner/worker/attempt checks and configurable durations.
3. Add expired-lease sweeper and retry policy with exponential backoff/jitter and clock injection.
4. Add failure taxonomy/error codes and safe diagnostic references.
5. Persist dead letters and implement authorized list/replay with idempotent audit/outbox behavior.
6. Test worker crash before ack, lease expiry, late heartbeat/result, retry exhaustion, permanent failure, duplicate replay, and clock boundary cases.

## Success Criteria

- [ ] A crashed worker's lease expires and a bounded retry can be scheduled without corrupting the newer attempt.
- [ ] Renewal is rejected after terminal/expired ownership and does not resurrect work.
- [ ] Retry delays/jitter are deterministic under injected RNG/clock tests and never exceed policy bounds.
- [ ] Permanent failures skip retries; exhausted retryable failures become dead-lettered.
- [ ] Dead-letter replay is authenticated, authorized, audited, and duplicate-safe.

## Validation

- Go unit/property tests with fake clock
- PostgreSQL concurrency tests for lease row locking
- RabbitMQ/worker crash integration scenario
- DLQ API replay test with authorization and duplicate submission

## Risk Assessment

- Risk: lease duration too short creates duplicate processing. Mitigation: document interval/expiry trade-off and require renewal margin; result acceptance remains lease guarded.
- Risk: sweeper races with successful result. Mitigation: compare lease/state in one transaction; terminal success wins only when valid current lease.

## Security Considerations

Worker identity is authenticated by broker/service credential and registration data; lease IDs are opaque; diagnostic details are redacted; replay cannot target another owner without admin policy.

## Next Steps

Phase 16 adds bounded progress events and cooperative cancellation on the lease foundation.

## Unresolved Questions

- None blocking; initial lease/heartbeat defaults will be documented from measured local processing latency and made configurable.
