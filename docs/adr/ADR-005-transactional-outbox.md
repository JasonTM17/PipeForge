# ADR-005: Transactional outbox

## Status

Accepted

## Context

Creating a job in PostgreSQL and publishing a RabbitMQ command are separate failure domains. Direct publish inside a database transaction cannot provide atomicity.

## Decision

Persist an outbox message in the same PostgreSQL transaction as the domain mutation. A separate publisher leases rows, publishes with confirms, and marks them published or schedules bounded retry.

## Consequences

Committed jobs are eventually publishable after broker outages. Publishing may duplicate a message around crashes, so consumers must be idempotent; outbox cleanup/retention is required.

## Alternatives considered

- Direct synchronous publish: loses commands on broker failure or delays the request transaction.
- Two-phase distributed commit: excessive complexity and poor fit for RabbitMQ/application portfolio scope.

