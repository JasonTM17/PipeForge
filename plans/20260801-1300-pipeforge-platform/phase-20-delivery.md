---
phase: 20
title: "Delivery"
status: pending
priority: P1
effort: "5d"
dependencies: [1, 2, 3, 9, 17, 18, 19]
---

# Phase 20: Delivery

## Overview

Finish production-oriented packaging: Go/Python multi-stage images, complete Docker Compose profiles, GitHub Actions validation/integration/image workflows, end-to-end processing journey, setup/API/operations docs, and a release-ready portfolio baseline.

## Requirements

- CI runs Go format/vet/test/race/build, Python format/lint/type/test/package, contract/schema validation, migrations, integration tests, image builds, secret/dependency scans.
- No cloud credentials required; integration tests use disposable containers or clearly documented service prerequisites.
- README commands match actual Makefile/Compose/CLI behavior.
- Release output records known limitations and does not claim unmeasured performance/HA.

## Architecture

Separate images for API, scheduler, result consumer, and Python worker use multi-stage builds, non-root users, minimal runtimes, health checks, and graceful shutdown. CI has fast PR jobs plus integration/image jobs; main workflow builds versioned artifacts without publishing secrets. E2E covers upload → idempotent job → outbox → worker → profile/quality/anomaly artifacts → result acceptance → authorized download.

## Related Code Files

- Create/modify: `infrastructure/docker/`, `compose.yaml`, `.dockerignore`
- Create: `.github/workflows/ci.yml`, `integration.yml`, `release.yml`
- Create: `go/tests/e2e/`, `python/tests/integration/`, fixtures and test harnesses
- Modify: `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `docs/`, `Makefile`
- Create: release checklist/version metadata; do not add generated credentials

## Implementation Steps

1. Build and inspect each runtime image with a non-root user, health command, dependency cache, and graceful signal handling.
2. Make Compose start the complete local stack with profiles and bounded defaults.
3. Add CI matrices and cache strategy; run contract/migration/secret/dependency checks before integration.
4. Implement e2e harness with real PostgreSQL/RabbitMQ/MinIO and at least one worker; add failure journey cases where runtime is stable.
5. Audit every README command, API route, config key, diagram, ADR, and runbook against code.
6. Run final test/review/security/clean-worktree gates and create a release note/tag only if all acceptance criteria pass.

## Success Criteria

- [ ] Docker Compose starts the whole platform from documented commands.
- [ ] E2E success journey and required failure journeys pass deterministically or report a documented environment limitation.
- [ ] GitHub Actions workflows are syntactically valid and do not require real cloud credentials.
- [ ] README, OpenAPI, contracts, architecture, security, testing, performance, and runbooks match implementation.
- [ ] Final code review has no critical/high unresolved issue; no critical TODOs/secrets remain.
- [ ] Release checklist includes commit history, validation evidence, limitations, and rollback notes.

## Validation

- `docker compose config` and clean `docker compose up --build`
- Go/Python/contract/integration/e2e test suites
- CI workflow syntax validation
- image user/health/vulnerability checks
- secret scan and `git diff --check`
- adversarial code review and fresh verification after fixes

## Risk Assessment

- Risk: e2e depends on timing-sensitive distributed services. Mitigation: health gates, polling with timeouts, injected IDs, deterministic fixtures, and explicit failure diagnostics.
- Risk: docs drift at the end. Mitigation: verify every command/path against code and include docs review as a release gate.

## Security Considerations

Do not publish images or artifacts with secrets; pin dependencies where practical, scan images, run as non-root, avoid privileged Compose settings, and keep release workflow permissions minimal.

## Next Steps

After this phase, mark the CK plan complete only after all acceptance criteria and fresh verification evidence are recorded; otherwise keep the goal active with explicit blockers.

## Unresolved Questions

- None blocking; registry publication and production deployment remain intentionally outside this local portfolio delivery unless separately authorized.

