# ADR-007: Job lease model

## Status

Accepted

## Context

A worker may crash after receiving a command. Queue acknowledgement alone cannot prove that work completed or distinguish stale results from a retry.

## Decision

Every attempt has an opaque lease with expiry and renewal. Progress/results include job, attempt, and lease IDs. Go accepts results only for the current valid lease and recovers expired leases with a bounded retry policy.

## Consequences

Worker failure becomes recoverable and stale results cannot overwrite newer work. Lease duration/renewal interval affect duplicate risk and recovery latency and must be measured/documented.

## Alternatives considered

- Long queue acknowledgement timeout: poor crash recovery and weak state ownership.
- Heartbeat-only failure detection: absence does not prove job failure; leases provide an explicit ownership window.

