---
phase: 16
title: Progress and cancellation
status: completed
priority: P1
effort: 3d
dependencies:
  - 14
  - 15
---

# Phase 16: Progress and cancellation

## Overview

Add throttled worker progress snapshots and cooperative user cancellation with race-safe state transitions, cancellation commands, cleanup, and tests.

## Requirements

- Progress is bounded by time/percentage change, not one message per row.
- Progress includes job/attempt/lease/stage/rows/estimate/percent/throughput/time and is validated/stale-safe.
- Cancellation is allowed only from non-terminal states; successful jobs cannot become cancelled.
- Worker checks cancellation between chunks/stages and publishes a cancellation result after cleanup.

## Architecture

Go stores latest progress snapshot plus capped history. API cancellation transaction marks `CANCEL_REQUESTED` and writes an outbox cancellation command. Python consumer maps job/attempt/lease to a cancellation token. Result consumer accepts cancellation only for the current attempt and valid transition.

## Related Code Files

- Create: `go/migrations/000011_progress_cancellation.sql`, progress repository/handlers
- Modify: Go scheduler/result consumer/queue, Python consumer/context/publisher
- Modify: progress/cancel JSON Schemas and OpenAPI
- Create: race/cancellation tests and sequence docs

## Implementation Steps

1. Define progress/cancel command/result schemas and rate limits.
2. Implement worker progress throttler with monotonic time/percentage thresholds.
3. Implement Go progress persistence and latest snapshot API projection.
4. Implement cancellation transition/outbox command and worker token checks/cleanup.
5. Add race tests for cancel vs success/failure/lease expiry and duplicate cancellation messages.

## Success Criteria

- [x] Progress volume remains bounded under large row counts and preserves latest state.
- [x] Cancellation stops work at a safe boundary and results in `CANCELLED` only when valid.
- [x] Success/cancel races have deterministic terminal outcome based on committed transition order.
- [x] Duplicate/stale progress/cancel/result messages do not regress state.

## Validation

- `go test ./...`, `go test -race ./...`, and `go vet ./...` pass.
- `go test -tags integration ./...` builds and is skip-safe; database-backed cases remain skipped because `PIPEFORGE_TEST_DATABASE_URL` is not configured.
- Python suite passes 54 tests; Ruff, format check, and mypy pass.
- Contract validation passes 14 schemas, 13 valid examples, and 4 invalid examples.
- Compose syntax validates with temporary non-secret required values; live Docker services were unavailable for end-to-end execution.
- API progress ownership/pagination and worker cancellation/progress behavior have focused tests.

## Risk Assessment

- Risk: cancellation is reported after success. Mitigation: Go transition guard treats success as terminal and worker result is advisory.
- Risk: progress backlog creates broker pressure. Mitigation: per-job throttling, latest snapshot storage, and bounded history.

## Security Considerations

Only authorized owners/admins can cancel; cancellation commands carry opaque IDs and no raw data; worker status/error logs remain redacted.

## Next Steps

Phase 17 provides `pipectl` access to the public API.

## Unresolved Questions

- No blocking implementation questions; progress history is capped at 100 newest entries per job in this phase and is not runtime-configurable yet.
