---
phase: 12
title: Data quality
status: in-progress
priority: P1
effort: 5d
dependencies:
  - 8
  - 10
  - 11
---

# Phase 12: Data quality

## Overview

Define and implement safe data-quality rule contracts, core null/unique/range/format/length/allowed-value/type checks, rule result aggregation, and quality report artifacts.

## Requirements

- Support the specified rule types except arbitrary custom expressions unless a safe bounded DSL is proven; keep `CUSTOM_EXPRESSION` rejected with a clear error otherwise.
- Rule result includes passed/failed/error/skipped, counts, percentage, duration, error code, and artifact reference.
- Rule configs and column scopes are validated at API and worker boundaries.
- Quality evaluation is chunk-based and does not return entire failure datasets through API.

## Architecture

`validators` contains a protocol plus one implementation per rule family. A rule engine compiles validated configs into pure chunk evaluators, aggregates counts, and writes bounded failure references/quarantine artifacts. Go stores rule metadata and authoritative job result projections; Python emits versioned quality artifacts/events.

## Related Code Files

- Create: `go/internal/quality/`, `go/migrations/000007_quality_rules.sql`
- Create: `python/src/pipeforge_worker/validators/`, quality result models
- Create/modify: quality contracts, API handlers, report serializer, tests
- Create: `python/tests/test_validators.py`, Go rule/auth tests
- Modify: OpenAPI, docs/security expression note

## Implementation Steps

1. Add data-quality rule/schema tables with owner/dataset scope, severity, enabled, configuration, and audit fields.
2. Define typed rule config models and reject unknown/unsafe config fields.
3. Implement NOT_NULL, UNIQUE, BETWEEN, MIN/MAX_LENGTH, REGEX, EMAIL_FORMAT, DATE_FORMAT, ALLOWED_VALUES, COLUMN_TYPE, ROW_COUNT_BETWEEN, and REFERENTIAL_SET as bounded evaluators.
4. Implement severity/result aggregation, capped failure references, and JSON quality artifact generation.
5. Add API CRUD with authorization and immutable job snapshot semantics for rule configs.
6. Add edge-case tests for nulls, invalid regex/date, duplicate keys, empty sets, type coercion, severity, and disabled/skipped rules.

## Success Criteria

- [ ] Supported rules produce correct counts/percentages across chunk boundaries.
- [ ] Invalid/missing columns classify deterministically and do not trigger infinite retries.
- [ ] Custom expressions cannot execute arbitrary code; unsupported requests fail closed.
- [ ] Quality artifacts are bounded, versioned, and available through authorized result paths.
- [ ] Rule CRUD and job snapshots preserve ownership/audit semantics.

## Validation

- Python validator tests and contract validation
- Go API/repository tests for CRUD/snapshot/authz
- cross-chunk aggregation fixtures
- security test attempting code injection in custom configuration

## Risk Assessment

- Risk: regex or referential checks cause CPU/memory abuse. Mitigation: compile with limits, bound regex length/input, cap reference sets, and enforce per-job resource budgets.
- Risk: rule config changes mid-job. Mitigation: persist an immutable operation/rule snapshot in the job request.

## Security Considerations

No `eval`/`exec`, no arbitrary imports, strict regex/input limits, protected artifacts, redacted failure samples, and per-user quotas for rule counts/reference data.

## Next Steps

Phase 13 adds IQR and Z-score anomaly detection using the same bounded numeric pipeline.

## Unresolved Questions

- None blocking; `CUSTOM_EXPRESSION` remains explicitly deferred until a safe expression language is designed and tested.

