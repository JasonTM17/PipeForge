# Validation evidence

Captured for the local runtime completion work on 2026-08-03.

| Gate | Command | Result |
|---|---|---|
| Go unit tests | `go test ./...` | pass before final asset/docs changes |
| Go race tests | `go test -race ./...` | pass in the runtime verification cycle |
| Go vet | `go vet ./...` | pass in the runtime verification cycle |
| Python full test suite | `python/.venv/Scripts/python.exe -m pytest -q` from `python/` | 60 passed |
| Python lint/format | `ruff check` and `ruff format --check` | pass |
| Python typing | `python/.venv/Scripts/python.exe -m mypy src` from `python/` | 65 source files pass |
| Contract fixtures | `python scripts/validate-contracts.py` | 15 schemas, 14 valid, 5 invalid fixtures pass |
| Console build | `npm run build` from `frontend/` | pass |
| Compose config | `docker compose config --quiet` | pass |
| Compose E2E | `make e2e` | pass: `SUCCEEDED`, 3 artifacts, download 200 |
| Service health | API and worker readiness endpoints | HTTP 200 |
| GitHub Actions | [run 30790719107](https://github.com/JasonTM17/PipeForge/actions/runs/30790719107) | pass: contracts, Python, console, Go (including race and vet) |

The E2E run used real PostgreSQL, RabbitMQ, MinIO, Go API/scheduler/result
consumer, and the active Python worker. The run created disposable local
records; no cloud account or production credential was used.

The GitHub Actions run above tested commit `fb9e96a`; the release package is
assembled from that verified source revision plus its release metadata.
