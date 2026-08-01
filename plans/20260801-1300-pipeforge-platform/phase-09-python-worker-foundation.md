---
phase: 9
title: "Python worker foundation"
status: pending
priority: P1
effort: "3d"
dependencies: [2, 7, 8]
---

# Phase 9: Python worker foundation

## Overview

Create the typed Python package, configuration, structured logging, health/metrics, RabbitMQ consumer, MinIO client, worker registration, and heartbeat publication without implementing dataset processing yet.

## Requirements

- Use `pyproject.toml`, type hints, Ruff, MyPy/Pyright, and Pytest.
- Worker acknowledges a job only after its outcome/result event is safely published or durably classified.
- Worker status and heartbeat data are bounded and do not include dataset rows or secrets.
- Shutdown drains intake, cancels/finishes in-flight work according to policy, and closes clients.

## Architecture

`python/src/pipeforge_worker` has contracts, consumers, messaging, storage, observability, and processing boundaries. Dependency injection passes settings, broker, storage, clock, and worker ID into services; no hidden global connection state. RabbitMQ consumer validates command envelopes and uses bounded concurrency/prefetch.

## Related Code Files

- Create: `python/pyproject.toml`, `python/src/pipeforge_worker/__init__.py`, package modules and `python/tests/`
- Create: `python/src/pipeforge_worker/config.py`, logging/metrics/health modules
- Create: `python/src/pipeforge_worker/messaging/`, `storage/`, `consumers/`, `contracts/`
- Create: `python/Dockerfile`, `python/README.md`
- Modify: `compose.yaml`, `.env.example`, `Makefile`, contract docs

## Implementation Steps

1. Add package metadata, dependency groups, Ruff/MyPy/Pytest configuration, and a typed settings model.
2. Implement structured logging/redaction, Prometheus metrics, health/readiness, and graceful shutdown signals.
3. Implement RabbitMQ connection/consumer/publisher abstractions with schema validation, manual ack, bounded retry, and trace headers.
4. Implement MinIO object adapter with streaming download/upload and attempt-scoped key helpers.
5. Implement worker registration/heartbeat event generation and a control loop with configurable interval/timeout.
6. Add unit tests using in-memory fakes for ack ordering, invalid messages, reconnect, heartbeat payload, and shutdown.

## Success Criteria

- [ ] `python -m pytest`, `ruff check`, `ruff format --check`, and type checking pass.
- [ ] Worker can start in health-only mode without a dataset.
- [ ] Invalid command messages are rejected/DLQ'd without crashing the consumer.
- [ ] Heartbeat payload satisfies the versioned contract and is throttled.
- [ ] No plaintext credentials, raw rows, or presigned URLs appear in logs.

## Validation

- Python unit tests with fake broker/storage
- JSON Schema contract tests against shared fixtures
- container health smoke test with RabbitMQ/MinIO
- graceful shutdown signal test

## Risk Assessment

- Risk: async libraries hide acknowledgement timing. Mitigation: wrap broker API behind explicit outcome/ack tests and document each callback boundary.
- Risk: worker process concurrency exhausts memory. Mitigation: semaphore/prefetch limits are config-validated and processing phase uses chunk bounds.

## Security Considerations

Use least-privilege broker/storage credentials, redact settings, validate message size/type, do not execute arbitrary config expressions, and run worker as non-root in the container.

## Next Steps

Phase 10 adds dataset readers and the chunk-based processing pipeline.

## Unresolved Questions

- None blocking; exact async RabbitMQ client version is pinned after the first integration smoke test.

