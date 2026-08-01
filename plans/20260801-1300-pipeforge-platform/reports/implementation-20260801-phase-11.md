# Phase 11 implementation report: profiling

## Scope

- Added bounded profile configuration to the shared job contract and both Go/Python validation boundaries.
- Added deterministic reservoir sampling, capped exact/approximate distinct counting, bounded common-value tracking, and sensitive-column suppression.
- Added dataset and column profile metrics: file/row/column counts, duplicate and missing counts, estimated chunk memory, duration, throughput, parsing warning count, inferred type, nullability, distinctness, numeric statistics, quantiles, string lengths, common values, and type violations.
- Added stable JSON serialization and attempt-scoped `profile.json` upload through the injected object-store abstraction.
- Added versioned `profile.report.v1` schema/example and performance documentation.

## Acceptance evidence

- Same input/config in test mode produces byte-stable profile output with injected monotonic clock and deterministic reservoir seed.
- Distinct state, reservoir size, frequency tracking, diagnostics, and common-value output are bounded; approximate distinctness and memory/sample warnings are explicit.
- Empty and mixed/null datasets report zero-safe throughput, unknown/unavailable metrics where appropriate, and no non-finite JSON numbers.
- Default profile output does not include common values or raw email-like values; sensitive columns stay suppressed even when common-value output is enabled.
- Artifact keys are generated as `reports/{jobId}/attempt-{attemptNumber}/profile.json`; canonical promotion remains outside Python.

## Verification

- `python/.venv/Scripts/python.exe -m pytest python/tests -q`: 37 passed.
- Ruff check/format: pass.
- MyPy strict: pass (`44` source files).
- Go `test ./...`: pass with the local Go 1.26.5 toolchain.
- Go `vet ./...`: pass.
- `python scripts/validate-contracts.py`: pass (11 schemas, 10 valid examples, 4 invalid examples).
- `node .claude/scripts/validate-docs.cjs docs/`: pass before the final report-only update.

## Docs impact

Major: added profiling bounds and artifact ownership to `docs/performance/chunk-processing.md` and messaging contract guidance.

## Unresolved questions

- Approximate distinctness is a bounded hash estimator; exact statistical error guarantees are intentionally not exposed as a stronger claim.
- Profile operation dispatch is available through injection, while the RabbitMQ command path remains fail-closed until lease-bound source/result coordination is implemented.
