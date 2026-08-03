---
title: PipeForge multi-worker scheduling and release hardening
status: completed
priority: P1
branch: main
created: 2026-08-03
---

# PipeForge multi-worker scheduling and release hardening

## Outcome

Replace the scheduler's configured single worker identity with a persisted,
heartbeat-driven worker pool. Lease assignment must select a live, capable
worker without exceeding its capacity, and job/cancellation commands must be
routed only to that worker. Add practical GitHub security gates and protect
`main` after the implementation is pushed and verified.

## Phases

| Phase | Scope | Status |
|---|---|---|
| 1 | [Worker registry and atomic selection](./phase-01-worker-registry-and-selection.md) | Completed |
| 2 | [Worker-targeted command routing](./phase-02-worker-targeted-routing.md) | Completed |
| 3 | [Security CI and repository policy](./phase-03-security-ci-and-repository-policy.md) | Completed |
| 4 | [Verification, docs, and release evidence](./phase-04-verification-docs-and-release.md) | Completed |

## Acceptance criteria

- The scheduler leases no job when every worker is stale, unhealthy, draining,
  offline, at capacity, or missing a required operation.
- Worker selection and lease assignment share one PostgreSQL transaction and
  row lock, so concurrent schedulers cannot oversubscribe a worker.
- Production lease acquisition has no explicit-worker bypass; every new lease
  must pass the same transactional registry eligibility path.
- Eligible workers are ordered by utilization, then least-recent assignment,
  with a deterministic UUID tie-break; existing owner fairness remains intact.
- Job and cancellation commands use worker-specific routing keys and queues;
  another worker cannot consume and dead-letter a valid command.
- The existing single-worker Compose E2E remains green and a two-worker test
  proves distinct registration plus safe targeted dispatch.
- Retry requests emit scheduler-facing `processing.job.queued`; only successful
  lease acquisition may emit a worker-targeted `processing.job.requested`.
- Go, Python, contracts, Compose, frontend, race, vet, security, and CI checks
  pass without weakening public HTTP or JSON payload contracts.
- `main` is protected against deletion/force-push and requires the verified CI
  checks, with a documented owner-bypass policy suitable for a solo repository.

## Constraints

- Go remains authoritative for worker eligibility, lease fencing, attempts,
  results, and job state.
- PostgreSQL is the worker registry source of truth; RabbitMQ events are
  at-least-once and may be duplicated or delayed.
- Do not add Redis, Kubernetes, cloud credentials, or a second lifecycle owner.
- Do not choose a license, production secret manager, backup/DR, or HA design in
  this plan; those require separate owner/deployment decisions.
- Preserve current API routes and version-1 message payload schemas.

## Rollback

Revert application and migration commits before deploying migration 000012.
After migration deployment, application rollback may leave the additive worker
registry table unused; do not drop it during an incident. Once suffixed command
routing is enabled, drain targeted queues or keep a compatible worker running
before rolling back; old workers cannot consume suffixed routes. Repository
protection can be relaxed through the GitHub settings/API if it blocks emergency
recovery.
