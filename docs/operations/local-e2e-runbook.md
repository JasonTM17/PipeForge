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

For the two-worker proof, run instead:

```powershell
make multi-worker-e2e
```

That target starts the `multi-worker` Compose profile, runs four complete jobs
concurrently, checks PostgreSQL for successful attempts assigned to at least
two distinct worker identities, validates exact RabbitMQ job/cancellation
bindings, confirms legacy queues have zero consumers, then cancels a real
leased/running large job and requires terminal `CANCELLED`.

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
- If a job remains `QUEUED`, inspect `docker compose logs scheduler worker` and
  query `processing_workers`. At least one worker must be `READY` or `BUSY`,
  have a heartbeat newer than `PIPEFORGE_WORKER_HEARTBEAT_TTL`, support every
  requested operation, and have an available active-attempt slot.
- If a job is `FAILED_RETRYABLE`, inspect worker logs for the bounded error
  code. Do not manually edit job state in PostgreSQL; the result consumer is
  the state-transition boundary.

## Teardown

```powershell
make down
```

`docker compose down` preserves named volumes. Use explicit volume removal
only when intentionally resetting local learning data.

## Shared-queue cutover

`PIPEFORGE_WORKER_LEGACY_SHARED_QUEUE_COMPATIBILITY=true` is an operator-managed
cutover setting. The operator must enable it on no more than one designated
worker while old `processing.jobs` and
`processing.cancellations` messages drain. New executable commands use
`processing.job.requested.<worker-uuid>` and new active cancellations use
`processing.job.cancel-requested.<worker-uuid>`.

After both legacy queues are empty, set the compatibility flag to `false` and
restart the primary worker. To roll back during the local transition, stop the
new scheduler/workers first, then re-enable the flag on exactly one worker.
Never run two shared-queue consumers because a worker cannot safely claim a
command fenced to another identity. The worker process does not coordinate or
enforce this deployment-wide singleton rule.

Every concurrently running worker also needs a unique
`PIPEFORGE_WORKER_ID`. `instanceId` prevents stale heartbeats from replacing a
new process in the registry, but version-1 command queues are keyed by worker
UUID. For an overlapping rollout, assign the new process a new worker UUID;
reuse an ID only after the old process has stopped intake.
