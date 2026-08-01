---
phase: 3
title: Go API foundation
status: completed
priority: P1
effort: 3d
dependencies:
  - 1
  - 2
---

# Phase 3: Go API foundation

## Overview

Establish a compilable Go module and HTTP service with configuration, structured logging, request correlation, readiness/health endpoints, Prometheus metrics, database connectivity, and the initial migration runner.

## Requirements

- Use explicit dependency injection and context cancellation.
- Return consistent problem-style errors with request IDs; do not leak internal errors.
- Keep migrations and database access in the Go control plane.

## Architecture

`go/cmd/api` wires config, logger, metrics, repositories, and HTTP routes. `go/internal/platform` owns shared runtime dependencies. `go/internal/observability` owns request IDs/metrics. `go/migrations` is append-only SQL. The API exposes `/health/live`, `/health/ready`, and `/metrics` before business endpoints.

## Related Code Files

- Create: `go/go.mod`, `go/go.sum`, `go/cmd/api/main.go`
- Create: `go/internal/platform/`, `go/internal/observability/`, `go/internal/httpapi/`
- Create: `go/migrations/000001_initial.sql`, `go/internal/database/`
- Create: `go/tests/` foundation tests and `docs/api/openapi.yaml`
- Modify: `Makefile`, `compose.yaml`, `README.md`

## Implementation Steps

1. Select and pin `chi`, `pgx/v5`, `prometheus/client_golang`, `slog`, and migration support; record reasons in ADR/tooling docs.
2. Implement typed environment configuration with fail-fast validation and safe redaction.
3. Add HTTP server lifecycle, graceful shutdown, correlation middleware, request logging, panic recovery, and problem responses.
4. Add liveness/readiness checks for process and PostgreSQL plus metrics counters/histograms.
5. Implement migration execution/status and a minimal database pool health test.
6. Add OpenAPI skeleton that only lists implemented endpoints.

## Success Criteria

- [ ] `go test ./...`, `go vet ./...`, and `gofmt` pass.
- [ ] API starts with valid environment and fails clearly with invalid required configuration.
- [ ] Readiness reports dependency failure without crashing the process.
- [ ] Request IDs are generated or propagated and appear in response/error/log context.
- [ ] An empty PostgreSQL database accepts migrations idempotently through the documented command.

## Validation

- `gofmt -w` + `gofmt -d`
- `go test ./...`
- `go vet ./...`
- migration run against a disposable Postgres container
- `curl` health/metrics smoke tests

## Risk Assessment

- Risk: shared packages become a dumping ground. Mitigation: keep platform wiring/observability small and domain code in later packages.
- Risk: readiness tests become flaky under Compose. Mitigation: use context timeouts and deterministic dependency fakes in unit tests.

## Security Considerations

Validate all configuration at startup, redact DSNs/passwords in logs, bound request bodies at middleware, and ensure panic recovery does not serialize stack traces to clients.

## Next Steps

Phase 4 adds identity and authorization on this HTTP/database foundation.

## Unresolved Questions

- None blocking; exact compatible dependency patch versions are resolved during implementation and recorded in `go.mod`.

