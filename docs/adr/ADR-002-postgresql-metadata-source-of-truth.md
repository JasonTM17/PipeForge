# ADR-002: PostgreSQL metadata source of truth

## Status

Accepted

## Context

Job transitions, ownership, idempotency, leases, outbox/inbox records, and audit history need ACID transactions and queryable relationships.

## Decision

Keep authoritative control-plane metadata in PostgreSQL. Raw datasets and generated artifacts live in MinIO, with object references and integrity metadata in PostgreSQL.

## Consequences

Transactions make state transitions and outbox/inbox processing explainable. PostgreSQL availability is a control-plane dependency; large object bytes are not stored in rows.

## Alternatives considered

- Document database: less natural integrity for lifecycle relationships and transactional history.
- Object metadata only: cannot safely coordinate idempotency, leases, and ownership.

