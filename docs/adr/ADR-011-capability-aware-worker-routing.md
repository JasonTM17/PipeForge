# ADR-011: Capability-aware worker routing

## Status

Accepted on 2026-08-03.

## Context

A shared worker command queue cannot safely carry commands that are already
fenced to a specific worker identity. Any consumer may receive the message,
and rejecting a command addressed to another worker can permanently discard
valid work. A static scheduler worker ID also prevents capacity-aware
distribution and makes stale workers look available.

## Decision

Workers publish registration and heartbeat events. The scheduler stores the
latest active instance, capabilities, lifecycle status, observed concurrency,
and heartbeat time in PostgreSQL. Lease acquisition locks the job attempt and
an eligible worker in one transaction. Eligibility requires a fresh heartbeat,
`READY` or `BUSY` status, all requested operation capabilities, and available
capacity derived from authoritative active attempts.

Executable and cancellation commands use worker-targeted topic keys and
durable queues suffixed with the canonical worker UUID. The legacy shared
queues are a bounded migration path only: operators must configure at most one
designated consumer and disable the compatibility flag after old messages
drain. This deployment-wide singleton is not enforced inside one worker
process.

## Consequences

- Two schedulers cannot over-assign the same worker slot through the normal
  lease path because selection uses row locks with `SKIP LOCKED`.
- A stale, draining, unhealthy, offline, incapable, or full worker receives no
  new lease.
- Commands cannot be stolen by a worker with a different identity.
- Registration/heartbeat delivery and PostgreSQL availability are now part of
  scheduling readiness.
- A worker UUID is a routing identity. Concurrent processes must not reuse it;
  overlapping rollouts assign a new UUID, while stop-before-start rollouts may
  reuse the stable identity.
- This is capacity and capability routing, not autoscaling, locality-aware
  placement, or multi-region orchestration.

## Verification

`make multi-worker-e2e` starts the optional second worker, executes four real
API-to-artifact jobs concurrently, verifies successful attempts used at least
two distinct worker identities, validates exact broker bindings and zero
legacy consumers, then completes a real targeted cancellation. Integration
tests exercise stale-event handling, capability selection, concurrent capacity
fencing, targeted cancellation routing, and retry routing.
