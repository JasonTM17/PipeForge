---
phase: 10
title: Processing pipeline
status: completed
priority: P1
effort: 4d
dependencies:
  - 5
  - 7
  - 9
---

# Phase 10: Processing pipeline

## Overview

Implement safe format detection and CSV/JSONL/Parquet readers with a processor protocol, bounded chunk iteration, bad-row policy, cancellation checks, and deterministic aggregation hooks.

## Requirements

- MVP supports CSV, JSON Lines, and Parquet; Excel remains deferred.
- CSV supports delimiter/header/encoding detection, quoted fields, malformed-row policy, and chunking.
- Readers do not load the full dataset by default and expose metadata/warnings without raw sensitive rows.
- Processing is dependency-injected and cancellation-aware.

## Architecture

`readers` converts object streams into typed chunk frames. `processors` orchestrates open → detect → metadata → infer → chunk loop → aggregation → report hooks. `ProcessingContext` carries job/attempt/lease IDs, settings, clock, cancellation token, and artifact sink. Reader and processor protocols permit deterministic unit tests.

## Related Code Files

- Create: `python/src/pipeforge_worker/readers/`, `processors/`, `contracts/operations.py`
- Create: CSV/JSONL/Parquet fixtures and `python/tests/test_readers.py`, `test_pipeline.py`
- Modify: worker consumer dispatch/factory and `pyproject.toml`
- Modify: shared job contract examples and processing docs

## Implementation Steps

1. Implement format detection from content type, extension hint, magic bytes, and bounded sniffing with explicit conflict handling.
2. Implement CSV reader with encoding/delimiter/header policies, malformed-row counters, and quarantine references that never log full rows.
3. Implement JSONL and Parquet readers with bounded iteration and schema metadata.
4. Implement processor protocol/context, operation dispatch, chunk aggregation, progress callback, cancellation check, and error classification.
5. Add deterministic fixtures and tests for empty files, quoted delimiters, malformed rows, mixed types, truncated JSONL, and cancellation.

## Success Criteria

- [x] All MVP readers process a fixture without full-file materialization.
- [x] `FAIL_FAST`, `SKIP_AND_REPORT`, and `QUARANTINE` policies produce documented outcomes.
- [x] Unsupported/malformed inputs classify as non-retryable and do not loop.
- [x] Cancellation stops between chunks and cleans temporary outputs.
- [x] Reader/pipeline tests are deterministic and type checked.

## Validation

- Pytest reader/pipeline suite
- Ruff/type checking
- memory-bound smoke test using a generated large CSV
- contract validation for operation configuration

## Risk Assessment

- Risk: malformed-row handling leaks data through diagnostics. Mitigation: emit row number/hash/reference only and cap quarantine metadata.
- Risk: Polars/Arrow versions change schema behavior. Mitigation: pin compatible versions and test inferred schema on representative fixtures.

## Security Considerations

Limit bytes/rows/chunks, reject decompression bombs or unsupported compression, sanitize generated CSV cells later, and never evaluate arbitrary Python code from operation config.

## Next Steps

Phase 11 builds profiling aggregators and report artifacts on the pipeline.

## Unresolved Questions

- None blocking; approximate distinct counting is optional and selected only where exact counting exceeds configured limits.
