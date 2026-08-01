# Results and artifacts

PipeForge treats worker results as an at-least-once event stream. The Go result consumer is the only component allowed to change authoritative job state or publish an artifact as public. Python workers upload attempt-scoped objects and publish references; they do not write PostgreSQL lifecycle tables.

## Event transaction

Each result delivery is processed in one PostgreSQL transaction:

1. Validate the common envelope and the strict result payload.
2. Insert `messageId` into `inbox_messages`; a duplicate becomes a no-op.
3. Lock the referenced job and attempt.
4. Require the current attempt, lease, and (for lifecycle terminal events) worker identity.
5. Apply the guarded lifecycle transition, progress snapshot, result projection, or staged artifact row.
6. Mark the inbox outcome and commit before acknowledging RabbitMQ.

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

## Storage and retention boundary

The result migration is `go/migrations/000009_results_inbox_artifacts.sql`. It adds inbox deduplication, result projections, attempt lease/worker identity, latest progress snapshots, and attempt-scoped artifact metadata. Lease expiry, retry scheduling, and dead-letter administration build on these columns in Phase 15.

Run the focused checks from `go/` with the repository's pinned Go toolchain:

```powershell
go test ./internal/result ./internal/httpapi ./internal/queue ./internal/platform/config ./internal/platform/migrations
```
