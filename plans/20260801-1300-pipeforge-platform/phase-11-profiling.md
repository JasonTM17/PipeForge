---
phase: 11
title: "Profiling"
status: pending
priority: P1
effort: "4d"
dependencies: [10]
---

# Phase 11: Profiling

## Overview

Add deterministic dataset/column profiling, configurable bounded samples and statistics, memory-aware aggregation, and JSON profile artifact generation/upload.

## Requirements

- Produce dataset metrics and column metrics required by the specification.
- Bound sample/common-value/high-cardinality output and make sensitive output configurable.
- Report parsing warnings, processing duration, throughput, and resource estimates.
- Artifact keys are attempt-scoped; canonical success promotion remains a Go decision.

## Architecture

`profilers` consumes chunk frames and updates typed aggregate state. Numeric/string/categorical aggregators are separate and testable. A report serializer emits versioned JSON with stable ordering/number handling. `reports` writes through the injected artifact sink; it does not mutate job state or publish success without the worker result coordinator.

## Related Code Files

- Create: `python/src/pipeforge_worker/profilers/`, `reports/`
- Create: profile contract schema/example and `python/tests/test_profiling.py`
- Modify: processing pipeline, operation dispatch, worker result composition
- Modify: `docs/performance/chunk-processing.md`, messaging/report docs

## Implementation Steps

1. Define profile model and operation config for sampling, quantiles, distinct strategy, common values, and memory caps.
2. Implement dataset-level and column-level aggregators with null/type/duplicate/memory/throughput metrics.
3. Implement bounded reservoir/common-value sampling with deterministic seed/test mode and redaction policy.
4. Serialize/upload profile JSON under `reports/{jobId}/attempt-{attemptNumber}/profile.json` via storage abstraction.
5. Add stable fixtures and tests for nulls, mixed types, empty columns, high-cardinality bounds, quantiles, duplicates, and repeatability.

## Success Criteria

- [ ] Profile output is deterministic for the same input/config in test mode.
- [ ] Memory/sample limits are enforced and visible as warnings.
- [ ] Required dataset/column metrics are present or explicitly marked unavailable.
- [ ] Profile artifact is attempt-scoped and contains no raw sensitive rows by default.
- [ ] Throughput/duration metrics use monotonic timing and handle zero-row input.

## Validation

- Pytest deterministic profile suite
- property tests for empty/single-row/all-null chunks
- artifact JSON Schema validation
- generated large fixture memory-bound smoke test

## Risk Assessment

- Risk: exact distinct counts consume unbounded memory. Mitigation: configured cap and optional approximate estimator with an accuracy note.
- Risk: numeric coercion silently changes values. Mitigation: preserve inferred type/violation counts and expose parsing warnings.

## Security Considerations

Do not output arbitrary frequent values from sensitive/high-cardinality columns by default; support column-level suppression and capped, redacted samples.

## Next Steps

Phase 12 evaluates data-quality rules over the same chunk stream.

## Unresolved Questions

- None blocking; exact approximate-distinct algorithm can remain a documented future adapter if MVP limits are sufficient.

