---
phase: 5
title: "Dataset metadata"
status: pending
priority: P1
effort: "4d"
dependencies: [2, 3, 4]
---

# Phase 5: Dataset metadata

## Overview

Add dataset and immutable dataset-version metadata, safe format/content validation, MinIO storage abstraction, and a streamed small-file upload path with ownership checks.

## Requirements

- Dataset versions are immutable; replacement creates a new version.
- API never buffers an entire upload in memory.
- Validate size, MIME/magic bytes where practical, filename safety, checksum, and allowed MVP formats.
- Raw objects use server-generated keys and are accessible only through authorized API paths.

## Architecture

`dataset` owns metadata/state transitions. `storage` exposes a narrow object-store interface implemented by MinIO. Upload handling streams request bodies through bounded readers to object storage while computing checksum. The DB transaction records metadata only after object write/verification succeeds, with cleanup on partial failure.

## Related Code Files

- Create: `go/internal/dataset/`, `go/internal/upload/`, `go/internal/storage/minio.go`
- Create: `go/migrations/000003_datasets.sql`
- Create/modify: dataset handlers/routes, OpenAPI dataset paths
- Create: upload/content validation unit and integration tests
- Modify: `compose.yaml`, `.env.example`, README/API docs

## Implementation Steps

1. Add dataset, dataset-version, upload metadata, state-history, and artifact-prefix schemas with owner/index constraints.
2. Implement object-store interface for put/get/head/delete and MinIO adapter with context/timeouts.
3. Implement filename normalization, content-type detection, magic-byte checks for CSV/JSONL/Parquet, max-size, checksum, and compression metadata.
4. Add dataset registration and streaming upload endpoint with transactional metadata and cleanup on failure.
5. Add list/get/delete semantics that preserve immutable historical versions and enforce ownership/scopes.
6. Add tests for malformed input, oversized body, path traversal names, checksum mismatch, cross-owner access, and stream behavior.

## Success Criteria

- [ ] CSV/JSONL/Parquet upload stores raw bytes in MinIO without full-memory buffering.
- [ ] A replacement produces a new immutable version; old version metadata remains readable.
- [ ] Invalid content/size/name/checksum is rejected without orphaned authoritative metadata.
- [ ] Dataset APIs paginate/filter and enforce ownership.
- [ ] No object key is derived directly from untrusted filename text.

## Validation

- Go unit tests for validators/storage contract
- MinIO integration test for put/head/get/delete
- HTTP upload test with a large generated stream and bounded read instrumentation
- migration test from empty database

## Risk Assessment

- Risk: a failed object upload leaves orphan bytes. Mitigation: best-effort cleanup plus a later orphan reconciliation hook; DB never points to unverified objects.
- Risk: format sniffing rejects valid files. Mitigation: treat sniffing as a layered check with explicit supported-format diagnostics and tests for representative fixtures.

## Security Considerations

Use opaque owner-scoped keys, short-lived presigned URLs only in multipart phase, size limits before storage, redacted metadata/logs, and no raw rows in errors or telemetry.

## Next Steps

Phase 6 adds large-file multipart lifecycle and abandoned-session cleanup.

## Unresolved Questions

- None blocking; exact magic-byte coverage is documented with any format limitations.

