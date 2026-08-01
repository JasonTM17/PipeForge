# PipeForge implementation plan

This document is the compact, repository-facing summary of the attached PipeForge specification. The authoritative execution tracker is [the CK plan](../plans/20260801-1300-pipeforge-platform/plan.md).

## Delivery order

1. Foundation and repository conventions
2. PostgreSQL, RabbitMQ, MinIO, Prometheus, and Grafana local infrastructure
3. Go API runtime, migrations, health, logging, metrics
4. Identity, refresh rotation, API keys, scopes, ownership
5. Immutable dataset metadata and streamed small uploads
6. Presigned multipart upload and expiry cleanup
7. Versioned JSON Schema contracts, RabbitMQ topology, outbox publisher
8. Job state machine, idempotency, fair scheduling
9. Typed Python worker, broker/storage adapters, heartbeats
10. CSV/JSONL/Parquet readers and bounded processing pipeline
11. Profiling and report artifacts
12. Data-quality rules and result artifacts
13. IQR/Z-score anomaly detection
14. Result consumer, inbox deduplication, artifact APIs
15. Leases, renewal, expiry recovery, retries, dead letters
16. Throttled progress and cooperative cancellation
17. `pipectl` public-API client
18. Upload hardening, quotas, rate limits, audit, threat model
19. Cross-service observability, dashboards, and runbooks
20. Images, CI, integration/e2e validation, docs audit, release readiness

## Non-negotiable invariants

- PostgreSQL is the control-plane source of truth.
- Python workers process data and publish events; they do not write core job state.
- Long-running work crosses the Go/Python boundary through versioned asynchronous messages.
- At-least-once delivery is expected. Consumers use inbox/idempotency and attempt/lease validation.
- Raw dataset versions are immutable. Artifacts are attempt-scoped until Go accepts a result.
- Dataset metadata is authoritative in PostgreSQL; raw bytes are private MinIO objects addressed only by server-generated keys.
- Retryable failures are bounded and jittered; permanent failures do not loop; dead letters are inspectable and replayable only through authorization.
- Public APIs expose controlled lifecycle commands, not unrestricted status mutation.

## Commit and verification policy

Each logical change gets its own Conventional Commit. Before committing:

1. Run the narrowest relevant test.
2. Run formatter/static checks for changed languages.
3. Validate schemas/migrations/infrastructure when touched.
4. Scan the staged diff for secrets.
5. Record the commit hash, message, changed components, commands, results, and next slice in the task report.

Do not create an empty commit merely to match a numbered list. If a baseline item is not real progress, combine it with the smallest adjacent behavior and explain why.
