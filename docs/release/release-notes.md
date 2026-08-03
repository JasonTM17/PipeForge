# Release notes — 0.2.3-local

## Highlights

- Lease-bound scheduler dispatches a server-resolved, schema-validated command.
- Scheduler persists worker registrations/heartbeats and selects a fresh,
  capable worker with available authoritative lease capacity.
- Executable and cancellation commands use worker-targeted queues; a real
  four-job Compose check proves distribution across two worker identities.
- Python worker executes real CSV processing and publishes profile, quality,
  and anomaly artifacts into an artifact bucket.
- Result consumer enforces inbox idempotency, attempt fencing, and canonical
  artifact promotion.
- Compose starts migration and all runtime services with health-aware ordering.
- `make e2e` provides a disposable API-to-artifact demonstration.
- CI covers Go, Python, contracts, documentation links, Compose configuration,
  frontend coverage/browser accessibility/build, and full multi-worker Compose
  E2E behavior.
- CodeQL, dependency review, Dependabot, secret push protection, and strict
  protected-`main` checks now guard repository changes.
- Vulnerable Go dependencies were upgraded and Dependabot security alerts were
  cleared at the verified source revision.
- `pipectl` provides safe API-only login, upload, job, dataset, artifact, and
  cancellation commands with a local ignored token file.
- Repository now includes architecture/lifecycle diagrams, an E2E GIF, a
  console, and release evidence.
- Five public multi-architecture images are available from both GHCR and Docker
  Hub with immutable digests, SBOMs, provenance, attestations, and runtime
  metadata checks.
- The repository is open source under the Apache License 2.0.

## Not included

This release does not include cloud deployment, production credentials,
autoscaling, multi-region availability, or an operational SLA.
