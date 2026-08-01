# Phase 13 implementation report: anomaly detection

## Scope

Implemented bounded IQR, population Z-score, and modified Z-score detection
with strict operation contracts, chunk-aware aggregation, reference-only output,
and attempt-scoped anomaly artifacts.

## Delivered

- Added typed anomaly config with safe column syntax, finite thresholds,
  `SKIP`/`FAIL` null policy, minimum sample bounds, and a 128-reference cap.
- Added deterministic pure detectors with explicit percentile interpolation,
  zero-variance handling, insufficient-sample handling, and no non-finite JSON.
- Added a bounded deterministic numeric retention strategy. Large inputs emit a
  `BOUNDED_NUMERIC_SAMPLE` warning and never serialize retained values.
- Added `anomaly.report.v1` schema/fixture, stable serializer, and
  `reports/{jobId}/attempt-{attemptNumber}/anomaly.json` upload helper.
- Added Python pipeline dispatcher and end-to-end unit fixtures; extended Go
  job validation and shared processing schema with anomaly options.
- Updated worker supported-operation defaults and chunk/job/messaging docs.

## Verification

- Python: `47 passed`.
- Ruff check and format check passed.
- MyPy strict passed for 57 source files.
- Contract validator: `13 schemas, 12 valid examples, and 4 invalid examples`.
- Go: `go test ./...` and `go vet ./...` passed.
- Detector tests cover IQR, Z-score, modified Z-score, chunk partition
  invariance, constant/insufficient data, null failure, invalid config, and
  pipeline dispatch.

## Docs impact

Minor: statistical methods, bounded sample behavior, operation options, and
artifact boundaries are documented. Public artifact access remains Phase 14.

## Unresolved questions

- None blocking; Isolation Forest remains explicitly out of scope.
