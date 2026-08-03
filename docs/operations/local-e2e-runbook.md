# Local end-to-end runbook

This runbook proves the real local path without cloud credentials. It is a
development demonstration, not a production deployment procedure.

## Start

```powershell
copy .env.example .env
make bootstrap
make dev
```

In a second terminal:

```powershell
make e2e
```

The script creates a disposable account, uploads a small CSV through the API,
creates three supported operations, waits for a terminal job state, verifies
three canonical artifacts, and downloads one artifact. It prints identifiers
and status only. It never prints credentials, presigned URLs, or source rows.

## What success proves

`SUCCEEDED` plus three artifact kinds (`profile`, `quality`, `anomaly`) proves
the following local links are alive:

1. API authentication and owner-scoped dataset access.
2. MinIO source upload and dataset-version metadata.
3. PostgreSQL job creation and `processing.job.queued` outbox signal.
4. Scheduler lease acquisition and fenced `processing.job.requested` command.
5. Python reader, Polars/Arrow operations, and immutable artifact upload.
6. RabbitMQ result delivery, inbox idempotency, and canonical metadata.
7. Authorized artifact listing and streaming download.

## Troubleshooting

- `docker compose ps` must show API and worker as healthy; migration must have
  exited with status 0.
- If queues contain messages from an earlier local run, inspect them before
  purging. The current result queue has exact lifecycle bindings and does not
  consume worker registration or heartbeat events.
- If a job remains `QUEUED`, inspect `docker compose logs scheduler` and verify
  the configured `PIPEFORGE_DISPATCH_WORKER_ID` is a UUID.
- If a job is `FAILED_RETRYABLE`, inspect worker logs for the bounded error
  code. Do not manually edit job state in PostgreSQL; the result consumer is
  the state-transition boundary.

## Teardown

```powershell
make down
```

`docker compose down` preserves named volumes. Use explicit volume removal
only when intentionally resetting local learning data.
