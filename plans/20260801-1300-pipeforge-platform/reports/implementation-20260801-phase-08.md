# Phase 8 implementation report: job management

## Scope

- Added PostgreSQL job, attempt, history, idempotency, and quota tables in migration `000007_jobs.sql`.
- Added explicit lifecycle validation, bounded operation contracts, canonical request fingerprints, owner/admin authorization, pagination, cancel/retry intent commands, and fair queued-job selection.
- Added transactional job creation with initial attempt/history/quota/idempotency/outbox records and rollback coverage.
- Added `/api/v1` job endpoints with `/v1` compatibility aliases and safe public projections.
- Tightened the shared `processing.job.requested.v1` JSON Schema and invalid fixtures.

## Acceptance evidence

- Idempotent replay returns the same job with HTTP `200`; conflicting fingerprints return deterministic `409`.
- Invalid transitions, ownership violations, unsupported operations, unknown config keys, oversized bodies, and unsafe public error fields are covered by tests.
- Scheduler fairness tests keep an older quiet owner eligible behind a noisy owner while preserving per-owner priority/age order.
- PostgreSQL integration verifies atomic job/attempt/history/idempotency/outbox creation and rollback when outbox insertion fails.

## Verification

- `go test ./...`: pass.
- `go vet ./...`: pass.
- `go test -race ./internal/job ./internal/scheduler ./internal/httpapi`: pass.
- `go test -tags=integration ./internal/job`: pass against the Compose PostgreSQL service.
- `python scripts/validate-contracts.py`: pass (10 schemas, 9 valid fixtures, 4 invalid fixtures).
- `node .claude/scripts/validate-docs.cjs docs/`: pass.
- Rebuilt Compose API image and ran an API smoke: readiness, CSV version upload, `/api/v1` job creation, idempotent replay, cancel, read/list, and error-field redaction all pass.

## Docs impact

Major: updated OpenAPI, control-plane/job-management architecture docs, messaging contract rules, and phase plan evidence.

## Cleanup

The smoke fixture user, dataset/version, job rows, outbox rows, quota counter, and MinIO raw object were removed with explicit scoped filters. No project source, Git metadata, or persistent service volume was removed.

## Unresolved questions

- Lease assignment, heartbeat, worker result acceptance, and timeout recovery remain intentionally deferred to Phases 14–16; this phase only selects bounded queued candidates without claiming a worker lease.
