# Release notes — 0.1.0-local

## Highlights

- Lease-bound scheduler dispatches a server-resolved, schema-validated command.
- Python worker executes real CSV processing and publishes profile, quality,
  and anomaly artifacts into an artifact bucket.
- Result consumer enforces inbox idempotency, attempt fencing, and canonical
  artifact promotion.
- Compose starts migration and all runtime services with health-aware ordering.
- `make e2e` provides a disposable API-to-artifact demonstration.
- CI covers Go, Python, contracts, Compose configuration, and the frontend
  build.
- `pipectl` provides safe API-only login, upload, job, dataset, artifact, and
  cancellation commands with a local ignored token file.
- Repository now includes architecture/lifecycle diagrams, an E2E GIF, a
  console, and release evidence.

## Not included

This release does not include cloud deployment, production credentials,
multi-region availability, an operational SLA, or a selected open-source
license.
