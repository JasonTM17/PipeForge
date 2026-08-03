---
phase: 2
title: Console correctness and accessibility
status: completed
priority: P1
effort: 8h
dependencies: []
---

# Phase 2: Console correctness and accessibility

## Overview

Make the operator console state-correct under overlapping requests and usable by
keyboard/screen-reader users, then protect the behavior with deterministic tests.

## Requirements

- Abort superseded refresh/artifact requests and ignore stale completions.
- Clear selected artifacts when jobs disappear, users sign out, or auth expires.
- Treat 401 as session expiry and return to sign-in with a useful live message.
- Add pending states, accessible form metadata, live regions, skip link, and robust text wrapping.
- Deep-link selected job in the URL without adding a routing dependency.

## Related code files

- Refactor: `frontend/src/App.tsx`, `frontend/src/api.ts`, `frontend/src/styles.css`.
- Create: focused components/hooks only where they reduce race-state complexity.
- Create: Vitest/Testing Library tests beside `frontend/src/`.
- Create: Playwright tests under `frontend/tests/` with isolated network fixtures.
- Modify: frontend package/config files and CI.

## Tests before

- Reproduce out-of-order artifact responses and stale refresh selection.
- Cover double-submit, request timeout/abort, 401 expiry, and sign-out during refresh.

## Implementation Steps

1. Add typed `APIError`, timeout/abort composition, and signal-aware API calls.
2. Centralize session reset and request generation/abort ownership.
3. Synchronize selected job with `job` query parameter and validate it against loaded jobs.
4. Split the page into readable semantic sections while keeping visual language.
5. Add accessibility metadata, status announcements, focus handling, and mobile text safety.
6. Add component and Playwright regression tests for every acceptance state.

## Success Criteria

- [ ] Latest job selection alone can update artifact state.
- [ ] Refresh cannot retain artifacts for a missing job.
- [ ] 401 clears token/data/URL and announces session expiry.
- [ ] Login cannot submit twice and all form fields expose proper metadata.
- [ ] Automated keyboard, responsive, error, empty, and long-content checks pass.
- [ ] Production build and dependency audit pass.

## Risk assessment

Abort handling can accidentally surface cancellation as user-facing failure. Treat
abort as control flow, keep real failures visible, and test StrictMode double effects.
