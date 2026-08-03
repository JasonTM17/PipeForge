# Known limitations

- The scheduler uses one configured learning-mode worker identity. Dynamic
  worker assignment and fairness across multiple workers are not implemented.
- Compose defaults are intentionally local and use development credentials from
  `.env.example`; rotate every value before any shared environment.
- The local object store, database, and broker have no production backup,
  disaster-recovery, HA, or SLO claim.
- The worker publishes immutable attempt objects. A late duplicate command can
  leave an unreferenced object until a future reaper is added; the result
  consumer will not make it canonical after a job has already succeeded.
- Progress delivery is bounded and may drop intermediate snapshots while
  retaining the latest useful state.
- The console is a small learning surface. It has no production session
  management, role administration, or browser E2E suite yet.
- License selection is intentionally deferred to the repository owner.
