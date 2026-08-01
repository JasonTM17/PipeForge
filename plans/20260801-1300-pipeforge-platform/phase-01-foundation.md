---
phase: 1
title: "Foundation"
status: pending
priority: P1
effort: "1d"
dependencies: []
---

# Phase 1: Foundation

## Overview

Create the real monorepo context for an otherwise empty workspace: Git baseline, README, reproducible tooling, directory boundaries, architecture overview, implementation plan, and initial ADRs.

## Requirements

- Preserve `.claude/`, `.codex/`, and project rules; do not commit local skill dependencies or secrets.
- Define Go control-plane/Python data-plane ownership and the asynchronous contract boundary before service code exists.
- Keep the first commit small and useful; follow with separate tooling and documentation commits.

## Architecture

The repository uses `go/`, `python/`, `contracts/`, `infrastructure/`, `docs/`, and `scripts/` as the stable top-level boundaries. `README.md` is the executable entry point; `docs/implementation-plan.md` mirrors the attached requirements while the persistent CK plan remains under `plans/`.

## Related Code Files

- Create: `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `Makefile`, `.gitignore`, `.editorconfig`, `.env.example`
- Create: `go/`, `python/`, `contracts/`, `infrastructure/`, `docs/`, `scripts/` directory markers and package roots
- Create: `docs/implementation-plan.md`, `docs/architecture/system-overview.md`, `docs/architecture/control-plane.md`, `docs/architecture/data-plane.md`
- Create: `docs/adr/ADR-001-go-control-plane-python-data-plane.md` through `ADR-010-monorepo-structure.md`
- Modify: none of the existing `.claude/` or `.codex/` skill files

## Implementation Steps

1. Initialize Git on `main`, define ignore rules for Go/Python/build/runtime data, and verify the worktree contains no pre-existing source to preserve.
2. Add root tooling conventions and a Makefile with placeholders only for commands that exist; document unavailable commands rather than faking them.
3. Write README setup, architecture, local prerequisites, quality gates, and the first vertical-slice roadmap.
4. Add Mermaid system-context/container diagrams and the initial ADR set with accepted trade-offs and rejected alternatives.
5. Run Markdown/link/path checks and a clean `git diff --check`; commit each logical group with explicit paths.

## Success Criteria

- [ ] `git status` is clean after foundation commits and the branch is `main`.
- [ ] README explains the actual current state, prerequisites, and next commands without claiming unimplemented services.
- [ ] Architecture documents state that PostgreSQL is authoritative and Python never mutates core job state directly.
- [ ] Ten ADRs record transport, storage, delivery, lease, dataframe, and monorepo decisions.
- [ ] No secrets, generated dependency trees, or local data are tracked.

## Validation

- `git diff --check`
- Markdown path/link scan via a repository script or PowerShell equivalent
- `make help` (or documented fallback if Make is unavailable)
- `git ls-files` audit for `.env`, keys, tokens, and local databases

## Risk Assessment

- Risk: empty-repo assumptions may cause docs to describe future behavior as present. Mitigation: label delivered vs planned commands and update README only after each slice is verified.
- Risk: CK tooling files are large and easy to stage accidentally. Mitigation: explicit staging and `.gitignore` rules for local tool output.

## Security Considerations

Do not include credentials in examples. Document secret injection through environment variables and make object-storage, database, and broker credentials placeholders only.

## Next Steps

Phase 2 adds runnable PostgreSQL, RabbitMQ, MinIO, and observability infrastructure using the boundaries established here.

## Unresolved Questions

- None blocking; the attached specification explicitly authorizes reasonable implementation decisions.

