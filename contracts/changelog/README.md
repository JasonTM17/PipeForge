# Contract changelog

Message contracts are versioned by message type and `schemaVersion`. A breaking
payload change requires a new schema version and a new compatibility window;
consumers must continue accepting the previous version until the migration is
complete.

## v1 — 2026-08-01

- Added the common envelope with message, trace, correlation, and causation IDs.
- Added processing job request/cancel/start/progress/success/failure/cancelled messages with attempt/lease fencing for active cancellation.
- Added worker registration/heartbeat and artifact-created events.
- Added the versioned profile report artifact schema and fixture.
- Added the versioned quality report artifact schema and fixture; quality rules
  reject arbitrary custom expressions and keep failure references bounded.
- Examples and negative fixtures are validated by `python scripts/validate-contracts.py`.
