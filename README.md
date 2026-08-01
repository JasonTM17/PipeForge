# PipeForge

PipeForge is a production-oriented distributed data-processing platform for uploading datasets, scheduling analysis jobs, processing data with Python workers, and accepting results through a reliable Go control plane.

> Status: foundation phase. The repository skeleton and architecture plan are present; service behavior is being delivered incrementally. Commands marked planned are not presented as working until their phase is implemented and tested.

## Architecture

The system is intentionally split into two planes:

- Go control plane: API, identity, PostgreSQL metadata, job state transitions, scheduler, leases, outbox/inbox processing, result consumption, audit, and administration.
- Python data plane: format readers, bounded chunk processing, profiling, data-quality checks, anomaly detection, artifact generation, and worker messaging.
- PostgreSQL is the authoritative metadata store. Python workers never mutate core job-state tables.
- RabbitMQ carries versioned asynchronous commands/events. MinIO stores immutable raw dataset versions and attempt-scoped artifacts.

See [the system overview](docs/architecture/system-overview.md), [control-plane boundary](docs/architecture/control-plane.md), and [data-plane boundary](docs/architecture/data-plane.md).

## Repository layout

```text
go/             Go API, scheduler, result consumer, CLI, migrations
python/         Typed Python worker and processing modules
contracts/      Versioned JSON Schema envelopes, examples, changelog
infrastructure/ Docker, RabbitMQ, MinIO, Prometheus, Grafana
docs/           Architecture, ADRs, security, testing, and runbooks
scripts/        Cross-platform development and validation helpers
plans/          Persistent implementation plan and phase reports
```

## Local prerequisites

The completed local environment will require:

- Docker Engine with Compose v2
- Go (version pinned in `go/go.mod` once Phase 3 lands)
- Python 3.12+ with a virtual environment
- GNU Make or the documented PowerShell equivalents

No cloud account or production credential is required for local development. `.env.example` currently contains blank placeholders; infrastructure phases will make the keys active. Copy it to `.env` locally when needed and never commit `.env`.

## Current local environment

The dependency stack is now runnable; Go/Python application services are added in later phases. Start PostgreSQL, RabbitMQ, MinIO, and the initialization jobs with:

```text
copy .env.example .env
make bootstrap
```

Optional monitoring services:

```text
docker compose --profile monitoring up -d prometheus grafana
```

Default host ports are isolated from common local stacks: PostgreSQL `55433`, RabbitMQ `55672`/management `55673`, MinIO `59010`/console `59011`, Prometheus `59090`, and Grafana `53010`. Change them in `.env` if needed.

The database migration and complete application startup commands (`make migrate`, `make dev` with Go/Python services) will be added only with their actual implementation and validation. You can inspect the plan and validate the worktree at any time:

```powershell
git status --short
Get-Content plans/20260801-1300-pipeforge-platform/plan.md
```

## Engineering rules

- Use explicit Conventional Commits, grouped by one logical behavior.
- Stage intended paths; do not use blind `git add .`.
- Run formatting, static analysis, tests, contract validation, and secret scanning before commits.
- Keep message consumers idempotent and use bounded retries/DLQs; never create infinite requeue loops.
- Treat upload data as untrusted. Do not log passwords, API keys, presigned URLs, object secrets, or full dataset rows.
- Record architecture decisions in `docs/adr/` and update docs when public behavior or architecture changes.

## Implementation plan

The full A-to-Z plan is [here](plans/20260801-1300-pipeforge-platform/plan.md), with one phase file per delivery boundary. The attached project specification is distilled into [docs/implementation-plan.md](docs/implementation-plan.md).

## License

License selection is intentionally deferred until the project owner chooses one. Do not assume an open-source license from this repository alone.
