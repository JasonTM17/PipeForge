# Phase 13 production review: anomaly detection

## Scope

Reviewed anomaly config, detector formulas, bounded aggregator, reference
hashing, report serializer, operation validation, shared schema, tests, and
documentation.

## Findings

### Critical / high

None found after Python static checks, full tests, Go validation tests, and
contract validation.

### Medium

The current pipeline uses a deterministic bounded numeric sample when input
exceeds the retention limit. The report makes this visible with
`numericSampleCount` and `BOUNDED_NUMERIC_SAMPLE`; it must not be described as
an exact whole-dataset anomaly count until a future two-pass/sketch implementation
is introduced.

Public artifact listing/download is intentionally deferred to Phase 14, where
Go will validate result ownership and attempt state before exposing artifacts.

## Evidence checked

- All detector inputs are finite before formula execution.
- IQR, Z-score, and modified Z-score zero-spread cases return `SKIPPED`.
- Null/non-numeric behavior is explicit and bounded by policy.
- Output references contain only row number and 16-character hash.
- JSON serializer rejects non-finite values and upload size is capped at 16 MiB.
- Go and Python reject unknown anomaly config fields and unsupported methods.

## Recommendation

Close Phase 13. Carry the bounded-sample disclosure and public artifact
authorization requirements into Phase 14 acceptance tests.

## Unresolved questions

- None blocking.
