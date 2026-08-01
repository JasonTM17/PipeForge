# ADR-009: Polars dataframe engine

## Status

Accepted

## Context

The worker needs CSV, JSONL, and Parquet support with bounded processing and profiling while keeping the first implementation understandable.

## Decision

Use Polars behind reader/processor interfaces. Prefer lazy/streaming or bounded chunk operations and keep engine-specific types inside the Python processing adapter boundary.

## Consequences

Columnar execution and low-copy paths are a good fit for large files. The team must pin compatible Polars/Arrow versions and document behavior where exact statistics differ from pandas conventions.

## Alternatives considered

- Pandas: broader familiarity and ecosystem, but easier accidental full-memory materialization.
- Hybrid engine from day one: more compatibility and complexity than the MVP needs.

