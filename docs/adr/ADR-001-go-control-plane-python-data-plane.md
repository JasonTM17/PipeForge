# ADR-001: Go control plane and Python data plane

## Status

Accepted

## Context

PipeForge needs reliable lifecycle orchestration and data-heavy processing with different runtime strengths. A single language would either obscure the distributed boundary or make data processing less practical.

## Decision

Use Go for API, identity, metadata, scheduling, leases, messaging coordination, and authoritative state. Use Python for readers, profiling, validation, anomaly detection, reports, and worker execution. Connect the planes through versioned asynchronous messages.

## Consequences

The boundary is explicit and demonstrates distributed-systems design. The repository carries two toolchains and requires contract tests; Python cannot directly mutate Go-owned job state.

## Alternatives considered

- Python for everything: simpler packaging, weaker demonstration of Go concurrency/control-plane ownership.
- Go for everything: stronger runtime uniformity, poorer fit for dataframe/statistical ecosystem.

