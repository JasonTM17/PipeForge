---
phase: 6
title: Multipart upload
status: completed
priority: P1
effort: 3d
dependencies:
  - 5
---

# Phase 6: Multipart upload

## Overview

Complete the large-file upload path with upload sessions, presigned part URLs, part tracking, completion/abort verification, immutable version promotion, and scheduled cleanup of abandoned sessions and MinIO multipart state.

## Requirements

- Presigned URLs are short-lived, scoped to one server-generated object key and upload session, and never expose credentials.
- Completion verifies part order/ETags, size/checksum metadata, object existence, and session ownership.
- Abort and expiry are idempotent; a completed upload cannot be aborted or mutated.

## Architecture

`UploadSession` and `UploadPart` are authoritative in PostgreSQL. MinIO multipart APIs are coordinated by Go. A cleanup job scans expired sessions with bounded batches, aborts remote multipart uploads, and records outcomes without blocking API requests.

## Related Code Files

- Create: `go/migrations/000004_multipart_uploads.sql`
- Modify: `go/internal/upload/`, `go/internal/storage/minio.go`, upload handlers/routes
- Create: `go/cmd/scheduler/` cleanup entrypoint or reusable cleanup service
- Create: multipart unit/integration tests and OpenAPI upload paths
- Modify: `compose.yaml`, `Makefile`, README/runbook

## Implementation Steps

1. [x] Add session/part schema with state, expiry, upload ID, expected size/checksum, and unique constraints.
2. [x] Implement initiate endpoint and per-part presigned URL instructions with bounded part size/count.
3. [x] Implement part registration with session ownership and ETag/size validation.
4. [x] Implement complete/abort handlers with transaction-safe state transition, remote verification, and dataset-version creation.
5. [x] Add cleanup command/loop with retry logging, metrics, and idempotent abort behavior.
6. [x] Add integration tests for out-of-order/missing parts, duplicate complete/abort, expired sessions, checksum mismatch, and cleanup reruns.

## Success Criteria

- [x] Multipart initiate → part upload → complete creates an immutable available dataset version.
- [x] Incomplete/expired sessions do not become available and are eventually cleaned.
- [x] Ownership and scope checks cover every session endpoint.
- [x] Presigned URL expiration and object key scope are testable/configurable.
- [x] Cleanup is bounded, observable, and safe to rerun.

## Validation

- MinIO multipart integration tests
- PostgreSQL migration/constraint tests
- API lifecycle tests with duplicate/reordered requests
- cleanup command smoke test against expired fixture sessions

Verified 2026-08-01:

- `go test ./...`, `go vet ./...`, and `go test -race ./...` passed in the Go 1.23 container.
- Tagged MinIO multipart integration passed against the Compose MinIO service.
- API smoke completed a two-part upload through host-reachable presigned URLs and promoted an `AVAILABLE` version.
- Scheduler smoke expired one generated fixture, failed its staged version, and passed an immediate idempotent rerun.
- `docker compose config --quiet`, OpenAPI YAML parsing, and docs validation passed.

## Risk Assessment

- Risk: remote completion succeeds while DB transaction fails. Mitigation: reconcile by verifying final object and retrying metadata promotion; never delete a known successful object blindly.
- Risk: client retries create duplicate sessions. Mitigation: optional idempotency key on initiation and unique active-session constraint per request fingerprint.
- Risk: cleanup races with a slow completion or transient MinIO outage. Mitigation: stale-completion grace window, explicit not-found classification, bounded retryable failures, and quarantine of an already completed object for reconciliation.

## Security Considerations

Short expiry, server-generated object keys, minimum/maximum part bounds, content-size validation, no public bucket, and redacted upload IDs/URLs in logs.

## Next Steps

Phase 7 defines the command/event contracts and durable messaging/outbox path used by jobs.

## Unresolved Questions

- None blocking; the exact minimum multipart size follows MinIO/S3 constraints discovered during integration testing.
