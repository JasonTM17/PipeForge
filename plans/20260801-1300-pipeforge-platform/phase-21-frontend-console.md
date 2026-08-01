---
phase: 21
title: Frontend console
status: pending
priority: P1
effort: 4d
dependencies:
  - 12
  - 14
  - 20
---

# Phase 21: Frontend console

## Overview

Build the API-backed PipeForge operator console from the Stitch dashboard design. The interface should make job execution, dataset health, quality rules, worker capacity, and artifacts legible at a glance without inventing data that the control plane does not expose.

## Design source

- Stitch project: `PipeForge/20260801-1300-pipeforge-platform`
- Screen: `15ba73c3a22b4e3aa4ff547c19bb7985`
- Export: [`./stitch-exports/dashboard/`](./stitch-exports/dashboard/)
- Required handoff: read `DESIGN.md` before implementation; preserve the dark graphite/electric-cyan visual language, dense data layout, and restrained component treatment.

## Requirements

- Use a typed frontend stack with a small, explicit API client.
- Implement responsive desktop/tablet/mobile layouts and keyboard-visible focus states.
- Cover loading, empty, error, unauthorized, and stale-data states.
- Use real API data for datasets, jobs, quality rules, workers, and artifacts; keep fixtures confined to tests.
- Keep secrets out of the browser bundle and never expose raw object-storage credentials.

## Implementation steps

1. Bootstrap the frontend app and design tokens from the Stitch export.
2. Implement shell/navigation, dashboard KPIs, job table/timeline, dataset health, and activity feed.
3. Add dataset, job, quality-rule, worker, and artifact views against `/api/v1` contracts.
4. Add authentication boundary, request correlation, retries, and safe problem rendering.
5. Add component, accessibility, responsive, and browser-flow tests.

## Acceptance criteria

- The dashboard visually follows the Stitch export while remaining responsive and keyboard navigable.
- No production view renders hard-coded fake operational values.
- API errors are rendered without leaking stack traces or secrets.
- Frontend lint, typecheck, unit tests, and browser smoke tests pass.

## Unresolved Questions

- None blocking; the initial screen is an operator dashboard and later pages may be split into smaller routes as the API surface grows.
