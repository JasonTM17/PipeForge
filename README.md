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

No cloud account or production credential is required for local development. Copy `.env.example` to `.env` only after the infrastructure phase adds it; never commit `.env`.

## Current foundation checks

At this foundation commit, the repository has no runnable service yet. You can inspect the plan and validate the worktree:

```powershell
git status --short
Get-Content plans/20260801-1300-pipeforge-platform/plan.md
```

After Phase 2, the documented startup path will be:

```text
make bootstrap
make migrate
make dev
```

The commands will be added only with their actual implementation and validation.

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

