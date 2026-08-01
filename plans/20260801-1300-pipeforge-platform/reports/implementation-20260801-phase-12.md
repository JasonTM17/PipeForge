# Phase 12 implementation report: data quality

## Scope

Implemented bounded data-quality rule contracts across the Go control plane,
Python worker, versioned contracts, API, storage migration, and documentation.

## Delivered

- Added `quality_rules` PostgreSQL migration with owner/dataset scope, active-name uniqueness, soft deletion, indexes, and audit writes.
- Added strict Go rule validation, owner-authorized CRUD service, `/v1` and `/api/v1` HTTP routes, nested dataset routes, pagination, and JSON boundary rejection.
- Added immutable `VALIDATE_QUALITY` job snapshots. Rule edits cannot change an already-created job operation.
- Added Python quality rule parsing, bounded row evaluators, cross-chunk aggregation, missing-column/disabled-rule states, capped row-number/hash failure references, and attempt-scoped `quality.report.v1` serialization/upload.
- Rejected `CUSTOM_EXPRESSION`, unsafe regex constructs, arbitrary configuration keys, unsafe column names, unbounded value sets, and non-finite numeric bounds.
- Updated OpenAPI, messaging/data-plane docs, quality guide, contract changelog, and report exports.

## Verification

- Go: `go vet ./...` passed.
- Go: `go test -race ./...` passed.
- Python: `41 passed`.
- Ruff check and format check passed.
- MyPy strict passed for 50 source files.
- Contract validator: `12 schemas, 11 valid examples, and 4 invalid examples`.
- OpenAPI YAML parsed successfully.

## Docs impact

Major: public quality-rule endpoints, job operation semantics, artifact contract,
and security boundaries are documented. Public artifact download remains a
Phase 14 result-processing responsibility.

## Unresolved questions

- `CUSTOM_EXPRESSION` remains intentionally deferred until a bounded expression
  language has its own grammar, resource budget, and security review.
