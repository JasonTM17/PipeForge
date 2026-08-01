---
phase: 14
title: Result processing
status: in-progress
priority: P1
effort: 5d
dependencies:
  - 7
  - 8
  - 9
  - 11
  - 12
  - 13
---

# Phase 14: Result processing

## Overview

Publish versioned worker started/progress/result/artifact events, implement the Go result consumer, add inbox idempotency and stale-result validation, and expose authorized profile/quality/artifact endpoints.

## Requirements

- Go, not Python, performs authoritative job transitions and metadata writes.
- Result processing transaction checks/records message ID, validates job/attempt/lease, applies transition, records artifacts/results/history, then acknowledges.
- Duplicate messages are no-ops; stale or wrong-lease results are safely ignored or quarantined with metrics.
- Public API never exposes internal stack traces or unauthorized objects.

## Architecture

The Python worker publishes a common event envelope. `go/cmd/result-consumer` consumes the results queue with manual ack. `InboxMessage` has a unique message ID and processing outcome. Result handlers use a transaction boundary that composes inbox insert, lease validation, transition, artifact rows, and history. Artifact download uses authorized, short-lived presigned URLs or streamed proxy responses.

## Related Code Files

- Create: `go/cmd/result-consumer/`, `go/internal/result/`, `go/migrations/000009_results_inbox_artifacts.sql`
- Modify: `go/internal/queue/`, `go/internal/platform/config/`, `infrastructure/rabbitmq/`, `infrastructure/docker/`, `compose.yaml`
- Create: canonical artifact handlers and OpenAPI result paths; profile/quality public artifact projections remain represented by the shared artifact contract
- Create: duplicate/stale result tests and integration fixtures

## Implementation Steps

1. Define started/progressed/succeeded/failed/artifact event decoding and worker result composition.
2. Implement consumer connection/reconnect/manual ack and bounded failure/DLQ policy.
3. Implement inbox insertion and result transaction with attempt/lease/state checks.
4. Record generated artifacts and promote only accepted successful artifacts to canonical references.
5. Add authorized profile/quality/artifact listing/download endpoints with pagination and range/size bounds.
6. Add unit/HTTP tests for malformed events, delivery acknowledgement policy, canonical visibility, ownership, scope, and bounded downloads; integration fixtures remain gated by local PostgreSQL/RabbitMQ/MinIO availability.

## Success Criteria

- [x] End-to-end success changes job state only after a valid result event is durably processed.
- [x] Redelivering the same result does not duplicate artifacts/history/state through inbox message-id deduplication.
- [x] Stale/expired lease results cannot overwrite a newer attempt.
- [x] Artifact APIs enforce ownership/scopes and avoid unbounded responses; profile/quality projections use the same canonical artifact contract.
- [x] Consumer ack occurs only after durable outcome or intentional DLQ classification.

## Validation

- Go unit/HTTP tests
- Existing RabbitMQ integration harness plus result-consumer topology declaration
- MinIO artifact download path with a bounded object reader; real MinIO verification requires the local stack
- End-to-end partial flow with a real worker/result consumer remains a delivery-gate check

## Risk Assessment

- Risk: acknowledging before DB commit loses a result. Mitigation: ack after commit only; reconnect/re-delivery is expected.
- Risk: canonical artifact pointer races with a later stale result. Mitigation: compare attempt/lease in the same transaction and monotonic transition guard.

## Security Considerations

Validate event origin/schema, isolate broker credentials, restrict artifact metadata/URLs, redact failure diagnostics, and enforce tenant/owner checks in service methods not only handlers.

## Next Steps

Phase 15 adds leases, expiration recovery, retries, and dead-letter administration.

## Unresolved Questions

- Local integration services were not started during this phase, so duplicate transaction and streamed MinIO verification remain explicit delivery-gate checks rather than silently reported as executed.
