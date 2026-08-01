---
phase: 13
title: Anomaly detection
status: completed
priority: P1
effort: 3d
dependencies:
  - 10
  - 11
---

# Phase 13: Anomaly detection

## Overview

Implement configurable IQR, Z-score, and modified Z-score anomaly detection with null/minimum-sample handling, bounded anomaly references, and report artifacts.

## Requirements

- Configuration includes column, method, threshold, null handling, minimum sample size, and sample output limit.
- Numeric conversion and zero-variance cases are explicit; no NaN/Infinity leaks into JSON.
- Entire anomalous datasets are never returned from the JSON API.

## Architecture

`anomaly` exposes pure detector functions over numeric aggregate/sample state. The pipeline performs a bounded first pass for statistics and a second bounded pass or retained references for classification as needed. Results are serialized to attempt-scoped artifacts and later accepted by the Go result consumer.

## Related Code Files

- Create: `python/src/pipeforge_worker/anomaly/`, `anomaly.report.v1`, anomaly reports
- Create: `python/tests/test_anomaly.py`
- Modify: operation dispatch, profile/result composition, shared schemas
- Modify: `docs/performance/chunk-processing.md`

## Implementation Steps

1. Implement numeric extraction/null policy and sample-size/variance guards.
2. Implement IQR, Z-score, modified Z-score with configurable thresholds and deterministic outputs.
3. Add bounded anomaly references and artifact serialization with warnings for insufficient data.
4. Add edge-case tests for all-null, one row, constant values, threshold boundaries, negative values, and NaN/Infinity.

## Success Criteria

- [x] Detector outputs match reference fixtures and are stable across chunk boundaries.
- [x] Insufficient/constant data returns a documented skipped/warning result, not a false anomaly storm.
- [x] Artifact size/sample bounds are enforced.
- [x] Invalid methods/configs are rejected at the contract boundary.

## Validation

- Pytest detector tests
- JSON Schema artifact validation
- property tests for chunk partition invariance

## Risk Assessment

- Risk: statistical formulas differ at boundaries. Mitigation: document percentile/mad convention and lock reference fixtures.
- Risk: outlier samples leak PII. Mitigation: output row references/hashed keys only unless explicit safe column policy allows values.

## Security Considerations

Cap sample output, suppress sensitive values, validate thresholds, and keep anomaly artifacts behind ownership/scope checks.

## Next Steps

Phase 14 connects worker result events to authoritative Go state and artifact APIs. It will expose authorized anomaly artifacts after result acceptance.

## Unresolved Questions

- None blocking; Isolation Forest remains a future adapter and is not required for MVP completion.
