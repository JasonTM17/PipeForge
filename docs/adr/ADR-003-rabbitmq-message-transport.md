# ADR-003: RabbitMQ message transport

## Status

Accepted

## Context

Go and Python must communicate asynchronously, with durable commands/events, acknowledgements, retries, and dead-letter routing in a local portfolio environment.

## Decision

Use RabbitMQ with durable exchanges/queues, persistent messages, publisher confirms, manual acknowledgements, bounded retry routing, and DLQs. Message envelopes are JSON Schema versioned.

## Consequences

The system demonstrates delivery semantics and can run locally with Docker. Queue topology and broker operations become part of the operational surface; Kafka is unnecessary for the initial scale.

## Alternatives considered

- Kafka: strong log/replay model but heavier local/operational footprint for this job-command workload.
- Direct HTTP calls: simpler happy path, couples long-running cross-language failure behavior and loses queue backpressure.

