---
phase: 4
title: Documentation and repository completion
status: completed
priority: P1
effort: 5h
dependencies:
  - 1
  - 2
  - 3
---

# Phase 4: Documentation and repository completion

## Overview

Make every public claim, plan status, security note, and release artifact match
the verified repository state.

## Requirements

- Add a concrete local threat model and document residual deployment risks.
- Correct README console setup/capabilities and API hardening configuration.
- Reconcile the original platform/runtime plans with superseding completed work.
- Replace the partial docs validator with full Markdown/link/config-aware coverage.
- Stop tracking generated ZIP packages; publish future binaries as release artifacts.
- Record fresh validation evidence at the final source revision.

## Related code files

- Create: `docs/security/threat-model.md`.
- Modify: `README.md`, `SECURITY.md`, design/runbook/release/security docs.
- Modify: original and runtime plan status/phase files through CK plan commands where supported.
- Modify: `.gitignore`, release package scripts/docs; remove tracked generated ZIP files.
- Modify: `.claude/scripts/validate-docs.cjs` only if project-local validator ownership permits; otherwise add `scripts/validate-docs.*`.

## Implementation Steps

1. Cross-reference code, OpenAPI, env, workflows, and docs before wording changes.
2. Write STRIDE-oriented local threat model with assets, boundaries, mitigations, and residual risks.
3. Reconcile plan completion/supersession through CK status operations and explicit notes.
4. Implement full docs/link/reference validation and add it to CI.
5. Move generated packages out of Git tracking without rewriting history.
6. Run docs validation, package/release checks, final review, and record evidence.

## Success Criteria

- [ ] No stale promised file, plan status, setup step, or UI capability remains.
- [ ] All Markdown relative links and documented code/config references validate.
- [ ] Threat model matches implemented auth/upload/messaging/runtime boundaries.
- [ ] Generated ZIP files are ignored and absent from the tracked tree.
- [ ] License remains clearly pending without implying open-source permission.
- [ ] Final evidence names exact source SHA, checks, limits, and unresolved owner decision.

## Risk assessment

Historical plans are evidence and must not be rewritten as if work happened earlier.
Mark supersession/completion with dated notes. Removing ZIPs affects only generated
artifacts; retain published release downloads and document regeneration.
