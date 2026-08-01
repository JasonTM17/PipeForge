---
phase: 19
title: "Observability"
status: pending
priority: P1
effort: "3d"
dependencies: [2, 7, 9, 14, 15, 18]
---

# Phase 19: Observability

## Overview

Complete metrics, structured logs, trace/correlation propagation, health/readiness, Prometheus/Grafana dashboards, and failure runbooks for PostgreSQL, RabbitMQ, MinIO, workers, leases, and dead letters.

## Requirements

- Cover HTTP, upload, jobs, queue depth, results, leases, workers, processing, bytes/rows, retries, and DLQ metrics listed in the specification.
- Propagate correlation/trace IDs through HTTP headers, DB context, RabbitMQ headers, worker logs, and result events.
- Never log passwords, API keys, presigned URLs, object secrets, full rows, or sensitive values.
- Dashboards and runbooks describe actionable thresholds without unrealistic benchmark claims.

## Architecture

Go and Python expose Prometheus metrics with bounded labels (no user/dataset/raw IDs as unbounded labels). Structured JSON logs share request/trace/correlation fields. Optional OpenTelemetry adapters remain no-op when not configured. Dashboards visualize queue depth/age, throughput, errors, leases, worker health, and DLQ growth.

## Related Code Files

- Modify: Go/Python observability packages, queue/storage/http middleware, result/worker events
- Create: `infrastructure/monitoring/grafana/dashboards/*.json`, alert/recording rules
- Create: `docs/runbooks/rabbitmq-unavailable.md`, `minio-unavailable.md`, `postgres-unavailable.md`, `worker-crash-loop.md`, `dead-letter-growth.md`
- Modify: architecture/deployment docs and README
- Create: metric/log redaction and trace propagation tests

## Implementation Steps

1. Inventory metric names/labels and implement missing counters/gauges/histograms with bounded cardinality.
2. Add message header trace context propagation and worker/result correlation.
3. Add dashboards for API, job lifecycle, queue/worker, storage, and data processing.
4. Add runbooks with symptom, evidence commands, safe mitigation, rollback, and data-loss caveats.
5. Test metrics endpoints, redaction, and propagation across a broker integration path.

## Success Criteria

- [ ] Metrics scrape successfully from Go and Python services.
- [ ] A job can be traced by correlation ID across API → outbox → worker → result consumer in logs/events.
- [ ] Dashboards use documented metric names and bounded labels.
- [ ] Runbooks cover all required dependency/failure cases and are executable from the local environment.
- [ ] No sensitive values are emitted in observability output.

## Validation

- Prometheus scrape/config check
- dashboard JSON validation
- integration trace/correlation assertion
- log redaction tests and manual sample inspection

## Risk Assessment

- Risk: high-cardinality labels exhaust Prometheus. Mitigation: allowlist low-cardinality labels and test metric families.
- Risk: tracing failures affect business work. Mitigation: propagation is best effort and never blocks durable state transitions.

## Security Considerations

Protect metrics/admin endpoints, redact labels/logs, avoid exposing object URLs, and document retention/access controls for telemetry.

## Next Steps

Phase 20 packages services, CI, e2e journey, documentation sync, and release readiness.

## Unresolved Questions

- None blocking; Jaeger/OpenTelemetry backend stays optional for local development.

