---
phase: 5
title: "Developer experience and operator console"
status: completed
priority: P2
effort: "3d"
dependencies: [3, 4]
---

# Phase 5: Developer experience and operator console

## Overview

Deliver a credible learning-product surface: `pipectl` exercises real public
APIs and a thin accessible operator console visualizes real platform state.

## File inventory

| Action | Files | Test impact |
|---|---|---|
| Create | Go CLI packages/command tests | HTTP/error/streaming tests |
| Create | a small frontend project and `docs/design-guidelines.md` | lint/type/browser tests |
| Modify | README/OpenAPI only for verified public behavior | CLI/UI documentation checks |

## Requirements

- CLI never touches PostgreSQL/RabbitMQ/MinIO; handles auth, upload, jobs,
  workers, artifacts, and DLQ through the public API only.
- Console uses typed API responses, handles loading/empty/error/unauthorized/
  stale states, and contains no operational fixture data or browser secrets.
- Both demonstrate the same e2e job rather than a separate fake walkthrough.

## Implementation Steps

1. Build/test `pipectl` around the existing OpenAPI contract, streaming upload
   and safe local token storage.
2. The referenced Stitch export is absent in this worktree; create a concise
   repository-owned design guideline before building a small typed console after
   API data shapes are stable.
3. Add accessible, responsive views for datasets, jobs, progress, artifacts,
   worker health, and DLQ summaries.
4. Add CLI fake-server tests and browser smoke tests against live local API.

## Success Criteria

- [x] `pipectl` and console read real API-backed dataset, job, and artifact data.
- [x] No UI/CLI path bypasses authorization or exposes storage credentials.
- [x] Console build and CLI unit tests pass.

Known scope boundary: the current public API has no worker-capacity endpoint,
so the console does not invent a worker panel. DLQ data remains available via
the existing owner/admin API and `pipectl` can be extended when a stable
operator contract is intentionally exposed.
