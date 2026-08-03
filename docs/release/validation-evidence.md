# Validation evidence

Captured for the local runtime completion work on 2026-08-03.

| Gate | Command | Result |
|---|---|---|
| Go unit tests | `go test ./...` | pass before final asset/docs changes |
| Go race tests | `go test -race ./...` | pass in the runtime verification cycle |
| Go vet | `go vet ./...` | pass in the runtime verification cycle |
| Python focused tests | `python/.venv/Scripts/python.exe -m pytest python/tests/test_job_runner.py python/tests/test_worker.py -q` | 13 passed |
| Python lint/format | `ruff check` and `ruff format --check` | pass |
| Python typing | `python/.venv/Scripts/python.exe -m mypy src` from `python/` | 65 source files pass |
| Contract fixtures | `python scripts/validate-contracts.py` | 15 schemas, 14 valid, 5 invalid fixtures pass |
| Console build | `npm run build` from `frontend/` | pass |
| Compose config | `docker compose config --quiet` | pass |
| Compose E2E | `make e2e` | pass: `SUCCEEDED`, 3 artifacts, download 200 |
| Service health | API and worker readiness endpoints | HTTP 200 |

The E2E run used real PostgreSQL, RabbitMQ, MinIO, Go API/scheduler/result
consumer, and the active Python worker. The run created disposable local
records; no cloud account or production credential was used.

Before tagging a release, rerun every command in this table from a fresh
worktree and replace this evidence with the resulting commit identifier.
