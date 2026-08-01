# Phase 11 review report: profiling

## Scope

- Reviewed profile configuration validation in JSON Schema, Go, and Python; aggregate memory bounds; deterministic sampling; numeric/JSON safety; artifact key scoping; and sensitive-value handling.

## Overall assessment

PASS for the Phase 11 scope. The implementation keeps domain aggregation behind the chunk protocol, caps state growth, serializes finite stable JSON, and leaves authoritative result acceptance to Go.

## Critical issues

None found.

## High priority issues

None found.

## Medium priority / deferred boundaries

- Approximate distinct counts use a deterministic bounded hash estimator after the configured cap. The report marks the value approximate and emits a warning; it does not imply an accuracy SLA.
- Duplicate row counting becomes explicitly unavailable after the configured distinct cap rather than pretending an exact result.
- Common-value output is opt-in and capped; per-column suppression remains authoritative when a column is listed as sensitive.
- Upload is tested through an injected fake store. MinIO promotion and Go result acceptance remain later-phase integration boundaries.

## Edge cases verified

- Empty input, nulls, duplicate rows, mixed inferred types, all-null columns, high cardinality, quantile sampling, sensitive columns, zero-duration processing, stable serialization, and attempt-scoped artifact keys.
- Non-finite numeric values are not allowed into serialized JSON.
- Go rejects unknown profile fields and out-of-bound nested values; the shared schema and Python parser enforce the same limits.

## Verification metrics

- Python: 37 tests passed; Ruff and MyPy strict passed.
- Go: all package tests and vet passed.
- Contracts: 11 schemas and fixtures passed.

## Unresolved questions

- Final report/result event shape will be reviewed again when Phases 12–14 add quality, anomaly, and authoritative result processing.
