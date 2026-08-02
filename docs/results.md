# Results and artifacts

PipeForge treats worker results as an at-least-once event stream. The Go result consumer is the only component allowed to change authoritative job state or publish an artifact as public. Python workers upload attempt-scoped objects and publish references; they do not write PostgreSQL lifecycle tables.

## Event transaction

Each result delivery is processed in one PostgreSQL transaction:

1. Validate the common envelope and the strict result payload.
2. Insert `messageId` into `inbox_messages`; a duplicate becomes a no-op.
3. Lock the referenced job and attempt.
4. Require the current attempt, lease, and (for lifecycle terminal events) worker identity.
5. Apply the guarded lifecycle transition, progress snapshot, result projection, or staged artifact row.
6. For a retryable failure, schedule the next attempt and delayed outbox command in the same transaction; when the budget is exhausted, persist a safe dead-letter record.
7. Mark the inbox outcome and commit before acknowledging RabbitMQ.

Stale, terminal, or wrong-lease events are recorded as `IGNORED` and do not mutate the newer attempt. Malformed messages are rejected to `control-plane.results.dlq`; processing failures are nacked without requeue so the broker's dead-letter route remains bounded.

## Artifact visibility

Artifact rows begin as `STAGED`. A successful result names the accepted object keys and promotes matching rows to `CANONICAL`; an artifact event may arrive before or after the success event. Public list, metadata, and download endpoints expose only canonical rows.

The API enforces `artifacts:read` and owner/admin access in the service layer:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/artifacts` | Paginated owner/admin list; optional `jobId` and `kind` filters. |
| `GET /api/v1/jobs/{jobID}/artifacts` | Paginated list scoped to one authorized job. |
| `GET /api/v1/artifacts/{artifactID}` | Canonical metadata without the internal object key or lease identity. |
| `GET /api/v1/artifacts/{artifactID}/download` | Authenticated streamed download with a 512 MiB bound; HTTP Range is not supported. |

Downloads use the object key held in PostgreSQL, never a client-supplied key. Missing objects are returned as a temporary storage error, while internal stack traces and diagnostic details stay server-side.

## Lease recovery, retries, and dead letters

Migration `go/migrations/000010_leases_retries_dlq.sql` adds the retry-ready timestamp, lease timing fields, and safe `job_dead_letters` records. Lease acquisition and renewal are row-locked and worker-scoped. The scheduler sweeper marks an expired attempt `TIMED_OUT`, creates a bounded next attempt, updates quota/history, and writes a delayed `processing.job.requested` outbox message atomically. The retry policy uses the bounded sequence `0s`, `10s`, `30s`, `2m`, and `10m` with at most 20% positive jitter.

Retryable result failures follow the same transaction boundary. Permanent failures skip retry scheduling; retryable failures that exhaust the job budget become `DEAD_LETTERED`. Dead-letter list and replay endpoints are owner-scoped for normal principals and admin-scoped for cross-owner operations:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/dead-letters` | Paginated open/all records with safe error metadata only. |
| `POST /api/v1/dead-letters/{recordID}/replay` | Authenticated, authorized, audited, duplicate-safe replay with a new attempt and outbox command. |

Successful, cancelled, failed, and dead-lettered jobs release their active quota slot. Queue admission remains bounded per owner; replay consumes a fresh queued slot and fails cleanly when capacity is exhausted.

## Storage and retention boundary

The result migration is `go/migrations/000009_results_inbox_artifacts.sql`; lease/recovery state is in `go/migrations/000010_leases_retries_dlq.sql`; bounded progress history is in `go/migrations/000011_progress_cancellation.sql`. Together they provide inbox deduplication, result projections, attempt lease/worker identity, latest progress snapshots, capped newest-first progress history, attempt-scoped artifact metadata, bounded retry scheduling, and authorized dead-letter administration.

Progress is exposed through `GET /api/v1/jobs/{jobID}/progress` and the paginated `/progress/history` route. The latest snapshot is replaced only by a newer event from the current attempt whose timestamp, processed rows, and percentage do not regress the stored state. History is trimmed transactionally to the newest 100 entries per job.

Run the focused checks from `go/` with the repository's pinned Go toolchain:

```powershell
go test ./internal/result ./internal/httpapi ./internal/queue ./internal/platform/config ./internal/platform/migrations
```
