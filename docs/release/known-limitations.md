# Known limitations

- Worker selection covers heartbeat liveness, operation capabilities, and
  lease capacity. It does not provide autoscaling, locality-aware placement,
  multi-region failover, or cluster-wide SLOs.
- Legacy shared queues remain declared for upgrade compatibility but have no
  consumer by default. A migration operator may temporarily enable one
  designated consumer to drain known-compatible old messages.
- Worker UUIDs are routing identities and must be unique across concurrently
  running processes. Use a new UUID for an overlapping rollout, or stop the old
  instance before reusing its UUID; instance IDs fence registry heartbeats but
  are not part of the version-1 job-command route.
- Compose defaults are intentionally local and use development credentials from
  `.env.example`; rotate every value before any shared environment.
- The local object store, database, and broker have no production backup,
  disaster-recovery, HA, or SLO claim.
- The worker publishes immutable attempt objects. A late duplicate command can
  leave an unreferenced object until a future reaper is added; the result
  consumer will not make it canonical after a job has already succeeded.
- Progress delivery is bounded and may drop intermediate snapshots while
  retaining the latest useful state.
- The console is a small learning surface. It has request cancellation,
  expired-session handling, unit coverage, and desktop/mobile browser E2E, but
  it intentionally has no refresh-token cookie flow, role administration, or
  full operational control surface.
- Authentication rate limiting is bounded but process-local. Multi-replica
  deployment requires a shared limiter and an explicit trusted-proxy policy.
- License selection is intentionally deferred to the repository owner.
