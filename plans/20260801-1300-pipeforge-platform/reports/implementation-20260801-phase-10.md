# Phase 10 implementation report: processing pipeline

## Scope

- Added strict operation parsing for the four versioned job operation types and bounded column/configuration validation.
- Added explicit format detection from Parquet magic bytes, content type, filename extension, and bounded text sniffing; conflicting signals fail closed.
- Added CSV and JSON Lines readers with incremental decoding, delimiter/header detection, quoted-field handling, encoding detection, bounded chunk frames, byte/row limits, and malformed-row policies.
- Added Parquet reading through PyArrow `ParquetFile.iter_batches`, converting each record batch to a Polars frame while keeping the source seekable only through a bounded memory/disk spool when required.
- Added an injected `ProcessingPipeline` with operation dispatch, chunk aggregation hooks, time-throttled progress, and cancellation checks between chunks.
- Added deterministic fixtures for empty input, quoted delimiters, Latin-1, mixed JSONL columns, truncated JSONL, malformed rows, non-seekable Parquet, byte limits, large CSV chunking, dispatch, and cancellation.

## Acceptance evidence

- CSV, JSONL, and Parquet fixtures are processed as bounded frames; the large CSV test processes 20,000 rows in 40 chunks of at most 500 rows.
- `FAIL_FAST` raises `MalformedRowError`; `SKIP_AND_REPORT` and `QUARANTINE` retain only capped row number/hash diagnostics and never raw row content.
- Unsupported, ambiguous, malformed, byte-limit, and row-limit failures use non-retryable reader exceptions.
- Cancellation is checked before each chunk and after iteration; no artifact sink is opened by this phase, so there are no temporary pipeline outputs to leak.
- Operation aggregators are dependency-injected; the pipeline itself does not mutate authoritative job state or publish a result.

## Verification

- `python/.venv/Scripts/python.exe -m pytest python/tests -q`: 32 passed.
- `ruff check python/src python/tests`: pass.
- `ruff format --check python/src python/tests`: pass.
- `mypy --strict python/src`: pass (`36` source files).
- Byte-bound and large-chunk reader tests: pass.

## Docs impact

Major: updated Python worker usage and data-plane architecture documentation with reader, policy, boundedness, and pipeline ownership rules.

## Unresolved questions

- Profiling, quality, anomaly, artifact, and result-event aggregators intentionally remain later phases; the pipeline accepts them through protocols rather than implementing domain behavior early.
- RabbitMQ command intake still fails closed until later phases provide lease-bound dataset source resolution and authoritative result publication.
