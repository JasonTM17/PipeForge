# ADR-006: Inbox idempotency

## Status

Accepted

## Context

RabbitMQ provides at-least-once delivery. Result events may be redelivered after a consumer crash or publisher retry.

## Decision

The Go result consumer inserts each message ID into a unique inbox table and applies state/artifact changes in the same transaction. Duplicate IDs are treated as already processed and acknowledged safely.

## Consequences

Duplicate delivery cannot duplicate authoritative transitions or artifacts. Inbox retention and storage growth need an operational policy; message IDs must be stable across retries.

## Alternatives considered

- In-memory deduplication: lost on restart and unsafe across replicas.
- Exactly-once broker assumption: not realistic; correctness must live at the consumer boundary.

