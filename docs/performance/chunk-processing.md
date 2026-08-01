# Chunk processing and profiling

PipeForge keeps dataset bytes out of PostgreSQL and avoids materializing a full
input in the Python worker. CSV and JSON Lines are decoded incrementally;
Parquet is read as bounded Arrow record batches and converted to Polars frames.
Reader options enforce byte, row, chunk, and diagnostic limits before domain
aggregators consume a chunk.

## Profiling bounds

`PROFILE_DATASET` accepts optional bounded configuration:

- `sampling`: deterministic `RESERVOIR` sampling with `maxRows` and `seed`.
- `quantiles`: at most 32 values in the inclusive range `0..1`.
- `maxDistinctValues`: exact values up to the cap, then a deterministic bounded
  hash estimator with an explicit `distinctApproximate` flag.
- `maxCommonValues`: bounded frequency tracking; output is disabled by default.
- `sensitiveColumns`: columns whose common values remain suppressed even when
  common-value output is enabled.
- `maxMemoryBytes`: recorded against estimated chunk memory and reported as a
  warning when the configured profile budget is exceeded.

Numeric sums, means, variance, min/max, string lengths, missing counts, and
bounded distinct/frequency state are updated per chunk. Quantiles use a seeded
reservoir so test-mode output is repeatable. No raw row or common value is
serialized by default.

## Artifact boundary

The profile serializer uses stable key ordering and rejects non-finite JSON
numbers. `upload_profile` writes only to
`reports/{jobId}/attempt-{attemptNumber}/profile.json`; Go owns acceptance and
canonical promotion after lease/result validation.
