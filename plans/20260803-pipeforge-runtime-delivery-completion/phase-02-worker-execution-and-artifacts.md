---
phase: 2
title: "Worker execution and artifacts"
status: completed
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 2: Worker execution and artifacts

## Overview

Activate the typed Python processing path in the real worker runtime. The
worker consumes only fenced commands, streams the immutable source, writes
attempt-scoped JSON artifacts, and publishes durable result events.

## Architecture

`ProcessingJobExecutor` replaces the rejecting handler. A small execution
adapter resolves the command's source with MinIO, runs the existing bounded
pipeline, serializes profile/quality/anomaly reports, uploads them under the
attempt prefix, and returns artifact descriptors. Progress is throttled and
published incrementally; cancellation is checked at chunk boundaries.

## File inventory

| Action | Files | Test impact |
|---|---|---|
| Modify | `python/src/pipeforge_worker/app.py`, `runtime.py`, `consumers/*` | active runtime tests |
| Create/modify | execution adapter and artifact publisher under `processors/` or `reports/` | source/artifact tests |
| Modify | `storage/minio.py`, contracts/examples when descriptors change | MinIO/contract tests |
| Modify | `python/tests/test_worker.py`, pipeline/storage tests | cancellation/progress/failure cases |

## Test scenario matrix

| Priority | Scenario | Proof |
|---|---|---|
| Critical | complete fenced CSV command | started/progress/artifact/succeeded sequence |
| Critical | missing/stale fence | rejected without artifact write |
| High | cancellation mid-stream | cleanup then cancelled, never succeeded |
| High | artifact upload failure | retryable failure event, no false success |
| Medium | long input | bounded progress memory and throttle behavior |

## Implementation Steps

1. Add tests proving the runtime installs a real executor in active mode and
   preserves health-only mode as an explicit diagnostic option.
2. Implement a narrow command-to-pipeline adapter using existing readers,
   processors, report serializers, storage interface, and cancellation registry.
3. Publish artifacts before terminal success with complete descriptors; make
   event ordering, error classification, and cleanup explicit.
4. Stream progress rather than retaining all snapshots until completion.
5. Run Python format/lint/type/unit tests plus contract validation.

## Success Criteria

- [ ] Active worker processes a valid fenced command end-to-end in tests.
- [ ] Artifacts are immutable, attempt-scoped, and describe size/content type.
- [ ] Cancellation, transient storage failure, and duplicate command behavior
      have deterministic tests.
- [ ] Health-only mode never consumes work; active mode never installs the
      permanent rejecting handler.

## Risks

Large datasets can amplify memory or event volume. Preserve chunked readers,
bounded progress emission, server-owned object keys, and no raw-row logging.
