---
phase: 7
title: Contracts and queue
status: completed
priority: P1
effort: 4d
dependencies:
  - 2
  - 3
  - 6
---

# Phase 7: Contracts and queue

## Overview

Define versioned JSON Schema envelopes and samples, declare RabbitMQ topology, implement Go publisher confirms, and add a transactional outbox publisher with bounded retries and recovery.

## Requirements

- Required envelope fields: message ID/type, schema version, occurred time, trace/correlation/causation IDs, and payload.
- Both Go producers/consumers and Python workers validate contracts before acting.
- Publishing never occurs inside the database transaction; the outbox row is committed with the domain mutation.
- Consumers acknowledge only after durable outcome; failures route through bounded retry/DLQ behavior.

## Architecture

`contracts/json-schema` is language-neutral. `contracts/examples` contains representative valid messages and invalid fixtures. RabbitMQ uses separate commands/events/DLQ exchanges with durable queues and persistent messages. `OutboxMessage` is leased by a publisher, confirmed by broker, marked published, and retried with backoff; it is safe for multiple publisher instances through row locking.

## Related Code Files

- Create: `contracts/json-schema/*.schema.json`, `contracts/examples/*.json`, `contracts/changelog/README.md`
- Create: `infrastructure/rabbitmq/definitions.json`, queue topology docs
- Create: `go/internal/queue/`, `go/internal/outbox/`, `go/migrations/000005_messaging.sql`
- Create: contract validation scripts and Go tests
- Modify: `Makefile`, `.env.example`, `docs/messaging/contracts.md`, `docs/messaging/queue-topology.md`

## Implementation Steps

1. [x] Define common envelope and job/worker/result/progress/cancel schemas with compatibility/versioning rules.
2. [x] Add positive/negative examples and a validation command usable in CI.
3. [x] Declare exchanges/queues/bindings, durability, dead-letter arguments, prefetch, and retry routing.
4. [x] Implement a typed Go RabbitMQ publisher with confirms, context timeouts, headers, and reconnect behavior.
5. [x] Add outbox repository/publisher loop with `FOR UPDATE SKIP LOCKED`, attempts, next-attempt time, and poison-message handling.
6. [x] Add duplicate/restart/reconnect/validation tests and document acknowledgement order.

## Success Criteria

- [x] All required message types have schemas, examples, and changelog entries.
- [x] Invalid envelope/payloads fail before publish/processing with actionable diagnostics.
- [x] Outbox domain transaction is independently testable and publisher recovery does not duplicate side effects beyond at-least-once delivery.
- [x] RabbitMQ topology is durable and has no infinite requeue loop.
- [x] Trace/correlation headers survive publication.

## Validation

- JSON Schema validation for every example
- RabbitMQ container topology inspection
- Go outbox unit tests and broker integration test with forced reconnect
- static scan that prevents unversioned contract additions

Verified 2026-08-01:

- `python scripts/validate-contracts.py`: 10 schemas, 9 valid examples, and 2 invalid examples passed.
- `go test ./...` and `go vet ./...`: passed in the Go 1.23 container.
- `go test -tags=integration ./internal/queue`: passed against the Compose RabbitMQ service, including forced reconnect.
- `go test -tags=integration ./internal/outbox`: passed against the migrated PostgreSQL service, including transaction enqueue, `SKIP LOCKED` lease, and wrong-token rejection.
- RabbitMQ CLI inspection confirmed three durable topic exchanges, eight durable queues, DLQ arguments, and bindings.
- `git diff --check`: pass.

## Risk Assessment

- Risk: schema drift between Go and Python. Mitigation: CI validates examples and both language consumers use the same schema fixtures.
- Risk: outbox starvation/duplicate publishing. Mitigation: indexed lease fields, bounded batch size, publisher confirms, and idempotent downstream consumers.

## Security Considerations

Validate message size and types, authenticate broker connections, avoid placing secrets or raw dataset records in payloads/logs, and isolate service credentials/permissions by queue role.

## Next Steps

Phase 8 adds job creation and scheduling on the outbox/queue foundation.

## Unresolved Questions

- None blocking; retry exchange mechanics may use delayed queues or scheduled republish, but must remain bounded and documented.
