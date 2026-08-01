# Contributing to PipeForge

PipeForge is delivered in small, reviewable slices. Read the root [README](README.md), the [implementation plan](docs/implementation-plan.md), and the relevant phase file before changing code.

## Change workflow

1. Keep the control-plane/data-plane boundary explicit.
2. Make one logical change at a time and add tests for success and failure paths.
3. Run the narrowest relevant checks, then the broader language/contract checks when a shared boundary changes.
4. Stage explicit paths; never use blind `git add .`.
5. Scan staged content for secrets before committing.
6. Use Conventional Commits under 72 characters, with no AI attribution.
7. Update architecture/API/security/runbook docs when behavior, contracts, or operations change.

## Commit examples

```text
feat(job): add idempotent job creation
test(queue): cover publisher reconnect recovery
docs(architecture): describe lease expiry flow
ci(github): validate Go Python and contracts
```

## Local quality gates

The available targets grow with each phase. Do not claim a target works until it has been implemented and run. At minimum, changed Go code must be formatted/vetted/tested; changed Python code must pass Ruff/type checks/tests; changed schemas/migrations/Compose files must be validated directly.

