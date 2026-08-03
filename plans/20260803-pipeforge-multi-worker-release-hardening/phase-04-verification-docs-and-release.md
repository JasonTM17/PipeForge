# Phase 4: Verification, docs, and release evidence

## Files

- Update architecture, messaging, operations, security, README, release known
  limitations, and the parent project plan to match implemented behavior.
- Add a concise implementation/review report under this plan's `reports/`.
- Refresh release evidence/package only after all gates pass.

## Validation

- Focused Go/Python tests, then full unit/race/vet/lint/format/type/build checks.
- Mandatory disposable PostgreSQL/RabbitMQ migration and integration gates for
  concurrent capacity, stale-event monotonicity, retry routing, cancellation
  targeting, and queue cutover compatibility.
- Mandatory real single-worker and two-worker Compose E2E proving registration,
  capacity-safe distribution, wrong-worker isolation, and targeted cancellation.
- Adversarial code review covering concurrency, stale events, wrong-worker
  routing, cancellation, error boundaries, and public contract compatibility.
- Clean worktree, pushed commits, green GitHub checks, and remote policy read-back.
