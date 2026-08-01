---
phase: 17
title: "CLI"
status: pending
priority: P2
effort: "3d"
dependencies: [4, 5, 8, 14, 15, 16]
---

# Phase 17: CLI

## Overview

Build `pipectl` as a typed Go client of the public API for authentication, dataset upload/listing, job lifecycle, worker visibility, and dead-letter administration.

## Requirements

- CLI never connects directly to PostgreSQL/RabbitMQ/MinIO.
- Commands have clear output, non-zero failure codes, configurable API endpoint, and safe token storage behavior.
- Upload command streams files and reports progress without printing secrets or raw data.

## Architecture

`go/cmd/pipectl` uses an API client package shared only within the CLI boundary. Auth stores access/refresh token material through a documented local mechanism with restrictive permissions; API keys can be supplied by environment/stdin without echo. Output has human and JSON modes.

## Related Code Files

- Create: `go/cmd/pipectl/`, `go/internal/client/`, CLI config/output helpers
- Create: CLI unit tests and command integration tests
- Modify: Go module, Makefile, README, API docs

## Implementation Steps

1. Implement endpoint client, auth refresh behavior, request IDs, error parsing, timeout, and retries only for safe/idempotent requests.
2. Add login and dataset list/upload commands with streaming file handling.
3. Add job create/get/cancel/retry and artifact/profile/quality commands.
4. Add worker/dead-letter list/replay admin commands with scope-aware errors.
5. Add text/JSON output, shell exit-code contract, and fixtures.

## Success Criteria

- [ ] Required commands work against documented API endpoints.
- [ ] CLI reports API problem responses clearly and exits non-zero on failure.
- [ ] Upload does not read full input into memory.
- [ ] Token/secret output is suppressed and local config permissions are checked.
- [ ] CLI integration tests cover expired auth, conflict, not found, forbidden, and network failure.

## Validation

- `go test ./...`
- CLI binary build and `--help`
- HTTP fake-server integration tests
- streaming upload test with generated input

## Risk Assessment

- Risk: retrying a non-idempotent request creates duplicate jobs. Mitigation: only retry with idempotency key and explicit safe method policy.
- Risk: CLI output becomes an undocumented API. Mitigation: stable JSON mode, concise human mode, tests for exit codes.

## Security Considerations

Use API endpoint TLS verification by default, restrictive local token file mode, no secrets in shell history guidance, and never print Authorization headers/presigned URLs.

## Next Steps

Phase 18 hardens uploads, quotas, rate limits, and audit visibility.

## Unresolved Questions

- None blocking; platform-specific secure token store can remain a follow-up if local encrypted file permissions are documented.

