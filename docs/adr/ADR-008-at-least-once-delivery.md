# ADR-008: At-least-once delivery

## Status

Accepted

## Context

Network, process, and broker failures can occur between side effects and acknowledgements. Strong exactly-once delivery is not available across PostgreSQL, RabbitMQ, MinIO, and Python execution.

## Decision

Assume at-least-once delivery everywhere. Make producers retry-safe, consumers idempotent, artifacts attempt-scoped, and state changes conditional on current job/lease identity.

## Consequences

The system tolerates duplicates and redelivery with explicit invariants. Duplicate work may still consume resources, so quotas, leases, deterministic keys, and observability are required.

## Alternatives considered

- Exactly-once claim: misleading across external systems and hides failure windows.
- Best-effort at-most-once: loses jobs/results during crashes and violates the reliability goal.

