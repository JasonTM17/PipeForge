---
phase: 3
title: Automated quality and release gates
status: completed
priority: P1
effort: 6h
dependencies:
  - 1
  - 2
---

# Phase 3: Automated quality and release gates

## Overview

Turn current manual evidence into repeatable CI/release gates and remove ambiguous
security status before publication.

## Requirements

- Run frontend unit and browser suites in CI, not build-only.
- Run single-worker and multi-worker real Compose E2E in a dedicated serialized job.
- Guarantee cleanup and upload concise logs/evidence on failure.
- Keep normal PR latency bounded through dependency caching and job timeouts.
- Adjudicate the current CodeQL opaque-token alert from cryptographic context.

## Related code files

- Modify: `.github/workflows/ci.yml`, `Makefile`, E2E scripts if cleanup/evidence needs hardening.
- Modify: `.github/workflows/codeql.yml` only if a narrow source annotation/query exclusion is justified.
- Add: reusable test scripts/configuration under `frontend/` and `scripts/`.
- Update: `docs/security/repository-gates.md`, release validation docs.

## Implementation Steps

1. Add frontend test/a11y/browser commands and CI artifacts.
2. Add full Compose E2E job with deterministic project name, timeout, and teardown.
3. Verify branch protection required checks after workflow naming changes.
4. Confirm SHA-256 only hashes 256-bit random opaque tokens while passwords use Argon2id.
5. Resolve CodeQL alert through the least invasive evidence-backed mechanism.
6. Run local CI-equivalent gates and inspect the resulting GitHub run after push.

## Success Criteria

- [ ] CI executes console unit, browser, build, and audit gates.
- [ ] CI executes real single/two-worker E2E with guaranteed cleanup.
- [ ] Required checks match actual workflow check names.
- [ ] Dependabot, secret scanning, and CodeQL have zero untriaged High alerts.
- [ ] No credential or source-row data appears in uploaded evidence.

## Risk assessment

Compose E2E may increase CI time and consume runner disk. Serialize it, use bounded
timeouts, prune only its named project, and upload small diagnostic artifacts.
