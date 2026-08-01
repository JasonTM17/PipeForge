---
phase: 2
title: "Infrastructure"
status: pending
priority: P1
effort: "1d"
dependencies: [1]
---

# Phase 2: Infrastructure

## Overview

Provide a deterministic local platform for the control plane and data plane: PostgreSQL, RabbitMQ, MinIO, Prometheus, Grafana, initialization, health checks, and environment validation.

## Requirements

- Docker Compose must start core dependencies without cloud credentials.
- Services use durable volumes, non-default application credentials from `.env`, bounded resources, and health checks.
- RabbitMQ topology and MinIO bucket/policy initialization must be repeatable and idempotent.

## Architecture

Compose defines dependency services separately from application profiles. RabbitMQ declares command/event/dead-letter exchanges and queues. MinIO initialization creates `datasets`, `artifacts`, and `quarantine` buckets with least-privilege local policies. Prometheus scrapes Go/Python metrics endpoints; Grafana loads a minimal dashboard profile.

## Related Code Files

- Create: `compose.yaml`, `infrastructure/docker/*.Dockerfile`, `.dockerignore`
- Create: `infrastructure/rabbitmq/definitions.json`, `infrastructure/rabbitmq/init.*`
- Create: `infrastructure/minio/init.*`, `infrastructure/monitoring/prometheus.yml`, `infrastructure/monitoring/grafana/`
- Create: `scripts/validate-env.*`, `scripts/bootstrap.*`, `scripts/wait-for-service.*`
- Modify: `.env.example`, `Makefile`, `README.md`

## Implementation Steps

1. Pin compatible image versions and define networks, volumes, health checks, profiles, and non-secret default variable names.
2. Declare RabbitMQ durability, manual-ack expectations, prefetch, dead-letter routing, and bounded retry queues.
3. Add MinIO bucket and policy initialization with safe object prefixes; verify reruns do not destroy data.
4. Add Prometheus/Grafana development profile and service readiness checks.
5. Add bootstrap/migrate/seed/dev targets only after each target has a real implementation.

## Success Criteria

- [ ] `docker compose config` succeeds with `.env.example` values.
- [ ] `docker compose up` starts PostgreSQL, RabbitMQ management, and MinIO healthily; monitoring profile is optional and documented.
- [ ] RabbitMQ exchanges/queues and MinIO buckets exist after repeated initialization.
- [ ] No service image runs as root where an application image is introduced.
- [ ] Credentials are environment-driven and absent from Git history.

## Validation

- `docker compose config`
- `docker compose up -d postgres rabbitmq minio`
- health/readiness probes and topology/bucket inspection
- `docker compose down` without removing named volumes

## Risk Assessment

- Risk: image tags drift. Mitigation: pin major/minor or digest where practical and document update procedure.
- Risk: startup ordering mistaken for readiness. Mitigation: health checks plus bounded wait scripts; applications still retry connections.

## Security Considerations

Use separate credentials per dependency role, disable unnecessary public MinIO access, avoid exposing management ports beyond local development, and document production TLS/secret-manager requirements without adding them to local Compose.

## Next Steps

Phase 3 consumes PostgreSQL and metrics contracts to establish the Go API foundation.

## Unresolved Questions

- None blocking; local image versions can be pinned during implementation after compatibility checks.

