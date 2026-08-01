# Phase 9 implementation report: Python worker foundation

## Scope

- Added the typed `pipeforge-worker` package with bounded Pydantic settings, structured redacted logging, Prometheus metrics, liveness/readiness, and graceful runtime shutdown.
- Added shared JSON Schema envelope validation with duplicate-key rejection and message-size limits.
- Added RabbitMQ publisher confirms, persistent messages, manual acknowledgement settlement, bounded consumer concurrency, reconnect handling, and queue declaration arguments matching the Compose dead-letter topology.
- Added MinIO streaming adapters and attempt-scoped object-key helpers without logging credentials, raw rows, or presigned URLs.
- Added worker identity, registration/heartbeat envelope generation, throttled heartbeat control, lifecycle state, health-only mode, and a non-root container image wired into the worker Compose profile.
- Added unit tests for configuration, observability, contracts, storage keys, ack ordering, invalid-message handling, heartbeat throttling, runtime shutdown, and health-only operation.

## Acceptance evidence

- Health-only Compose worker starts without a dataset and reports healthy liveness/readiness on host port `58090`; metrics expose the bounded worker status.
- Direct active-mode smoke starts against the running Compose RabbitMQ/MinIO services and logs both `RabbitMQ publisher connected` and `RabbitMQ consumer started`.
- The first active smoke exposed a real RabbitMQ queue-argument mismatch (`PRECONDITION_FAILED`); the consumer now declares the configured dead-letter exchange and routing key, and the repeat smoke completed without that error.
- Invalid or unsupported command messages are classified for non-requeue rejection; the broker's durable dead-letter topology is responsible for DLQ routing.
- Heartbeat payloads contain bounded worker/job state only, and settings projections redact broker URLs, storage credentials, and secret values.

## Verification

- `python -m pytest`: 20 passed.
- `ruff check python/src python/tests`: pass.
- `ruff format --check python/src python/tests`: pass.
- `mypy --strict python/src`: pass (`22` source files).
- `docker compose --env-file .env.example --profile worker build worker`: pass.
- Health-only Compose smoke: pass.
- Active RabbitMQ/MinIO smoke: pass after DLX queue-argument fix.
- `git diff --check`: pass.

## Docs impact

Major: documented the Python data-plane boundary, delivery/ack rules, worker configuration, and local worker Compose usage.

## Unresolved questions

- Valid job commands are deliberately classified to DLQ until Phase 10 installs the real processing handler.
- Result-event publication, control-plane registration persistence, lease fencing, cancellation, and recovery remain later-phase integration work.
