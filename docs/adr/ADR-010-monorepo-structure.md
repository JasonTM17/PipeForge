# ADR-010: Monorepo structure

## Status

Accepted

## Context

Go, Python, contracts, infrastructure, docs, and tests must evolve together while preserving explicit ownership and reviewable commits.

## Decision

Use one repository with separate `go/`, `python/`, `contracts/`, `infrastructure/`, `docs/`, and `scripts/` roots. Shared behavior crosses languages only through versioned contract fixtures and integration tests.

## Consequences

Cross-service changes are reviewable in one commit series and CI can validate compatibility. Toolchains remain isolated; path ownership and staged commits prevent unrelated changes from being bundled.

## Alternatives considered

- Separate repositories: stronger deployment isolation, slower contract changes and less coherent portfolio presentation.
- One mixed source tree: less explicit boundaries and higher accidental coupling.

