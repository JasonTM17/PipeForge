# Validation evidence

Captured for `0.2.0-local` on 2026-08-03. Source revision: `ddf4347`.

| Gate | Result |
|---|---|
| Go unit, vet, race | pass across all packages on Go 1.25 |
| Go DB integration | migrations plus outbox, job, lease, result, and worker-registry packages pass against disposable PostgreSQL |
| Go broker integration | queue topology tests pass against RabbitMQ |
| Python lint/format/type | Ruff pass; mypy pass across 65 source files |
| Python tests | 74 passed; pytest-asyncio emits Python 3.16 deprecation warnings only |
| Contracts | 15 schemas, 14 valid examples, 5 invalid examples pass |
| Console | TypeScript and Vite production build pass |
| Compose | configuration and Go 1.25 image builds pass |
| Single-worker E2E | job `SUCCEEDED`, three artifact kinds, authorized download HTTP 200 |
| Two-worker E2E | four concurrent jobs `SUCCEEDED`, assigned across two worker UUIDs, exact targeted bindings, zero legacy consumers |
| Cancellation E2E | active job reached `CANCELLED` through its assigned worker route |
| Review | adversarial re-review reports no actionable finding |
| GitHub CI | [run 30799562214](https://github.com/JasonTM17/PipeForge/actions/runs/30799562214): all five jobs pass |
| CodeQL | [run 30799562229](https://github.com/JasonTM17/PipeForge/actions/runs/30799562229): Go, Python, and JavaScript/TypeScript pass |
| Repository policy | strict eight-check `main` protection read back; force-push/deletion disabled; owner bypass documented |

The local E2E used real PostgreSQL, RabbitMQ, MinIO, Go services, and Python
workers. No cloud account or production credential was used. A first
integration attempt against the already-running Compose database was invalid
because active services could consume fixtures; the authoritative rerun used a
disposable isolated PostgreSQL instance and passed. CI integration packages run
with `-p=1` because package fixtures intentionally share one service database.

The release remains a verified local learning release, not production
certification. See [known limitations](./known-limitations.md).

## Professional hardening verification

The 2026-08-03 hardening branch additionally passed:

- all Go unit tests, race tests, vet and build after HTTP timeout and auth
  rate-limit controls were added;
- 10 console unit tests with 97.95% statement/line, 95% function and 88.42%
  branch coverage;
- 4 Chromium browser flows across desktop and mobile, including an axe scan
  with zero violations in the authenticated operator flow;
- npm production dependency audit with zero vulnerabilities;
- real Compose upload/process/artifact/download E2E and a two-worker run with
  four successful jobs, two assigned worker UUIDs, exact targeted bindings,
  zero legacy consumers and a real cancellation ending in `CANCELLED`;
- documentation link validation across 37 Markdown files and all contract
  fixtures;
- direct GitHub API verification of five public GHCR packages linked to this
  repository and the successful five-image container release workflow.

The GitHub CI run for the merged hardening revision is recorded in the merge
request rather than predicted in this pre-merge evidence.
