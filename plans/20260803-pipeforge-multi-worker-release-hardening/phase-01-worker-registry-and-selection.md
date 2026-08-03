# Phase 1: Worker registry and atomic selection

## Files

- Create `go/migrations/000012_worker_registry.sql`.
- Create `go/internal/workerregistry/` models, event parser, repository, selector,
  unit tests, and PostgreSQL integration tests.
- Modify `go/internal/lease/` to require an eligible worker through an injected
  transaction-scoped selector for every production acquisition; remove the
  explicit worker field from the scheduler/lease command boundary.
- Modify `go/internal/scheduler/`, `go/cmd/scheduler/`, and platform config.
- Modify queue topology so scheduler consumes registered/heartbeat events.

## Invariants

- Duplicate events are idempotent and an older heartbeat cannot replace newer
  worker state.
- Registration alone is not dispatch-ready; READY/BUSY heartbeat freshness is
  required.
- Heartbeat liveness defaults to 45 seconds (three default heartbeat periods)
  and remains bounded/configurable by the scheduler.
- A newer registration replaces the active instance for a worker ID; heartbeat
  events are accepted only for that active `instanceId`, and older timestamps
  cannot move state backwards.
- Worker capability must cover every operation in the job.
- Active LEASED/RUNNING attempts count against declared max concurrency.
- The selected worker row remains locked until lease assignment and outbox
  insertion commit or roll back together.
- Lock order is job/attempt first, then worker row. Selection atomically updates
  `last_assigned_at` before the same transaction writes the lease and outbox.
- Scheduler consumes durable `control-plane.workers` bindings for registration
  and heartbeat. It acknowledges only after a committed idempotent upsert;
  malformed events are rejected without requeue to the bounded worker DLQ.

## Validation

- Parser validation for malformed, oversized, stale, and duplicate events.
- Selector integration tests for liveness, status, capability, capacity,
  deterministic fairness, and concurrent acquisition.
- Scheduler unit tests prove no static worker ID or explicit-acquisition bypass
  remains.
