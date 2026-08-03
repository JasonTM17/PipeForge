# Professional hardening verification

Date: 2026-08-03
Branch: `improvement/professional-hardening`
Status: local gates passed; remote PR gates pending

## Delivered

- API server header/read/write/idle timeouts and maximum header size.
- Bounded process-local auth token bucket on register, login, and refresh.
- Ten-minute `pipectl` request timeout.
- Console request cancellation, session generation fencing, 401 cleanup,
  selected-job URL state, stale-selection cleanup, responsive accessibility
  fixes, and explicit live regions.
- Frontend unit coverage and desktop/mobile Chromium + axe browser flows.
- CI production dependency audit, frontend coverage/browser/build gates,
  documentation links, and real multi-worker Compose E2E.
- Threat model, secure configuration guide, release-document synchronization,
  and removal of tracked generated ZIP archives.
- Worker selection serialization fix discovered by isolated integration testing.

## Verification evidence

| Gate | Result |
|---|---|
| Go unit | pass across all packages |
| Go race | pass across all packages |
| Go vet/build | pass |
| Go integration | migrations plus outbox/job/lease/result/worker-registry/queue pass on isolated PostgreSQL |
| Capacity race regression | 20 consecutive isolated runs pass |
| Python | Ruff lint/format, mypy 65 files, pytest 74 tests pass; Python 3.16 deprecation warnings remain |
| Contracts | 15 schemas, 14 valid examples, 5 invalid examples pass |
| Documentation | 37 Markdown files pass local-link validation |
| Console unit | 10 tests pass; 97.95% lines/statements, 95% functions, 88.42% branches |
| Console browser | 4 Chromium flows pass across desktop/mobile; authenticated axe scan has zero violations |
| npm production audit | zero vulnerabilities |
| Compose core E2E | `SUCCEEDED`; three artifacts; authorized download HTTP 200 |
| Compose multi-worker | 4 jobs on 2 worker UUIDs; exact targeted bindings; zero legacy consumers; cancellation `CANCELLED` |
| Packages | five public GHCR packages linked to `JasonTM17/PipeForge`; release workflow 30809299824 passed all publish jobs |
| CodeQL | sole open high alert verified as a false positive for a 256-bit opaque token lookup digest and dismissed with rationale |

## Race root cause

Worker capacity and row locking were evaluated in one PostgreSQL statement.
Concurrent statements could both observe zero active leases before lock
completion. Selection now locks a static eligible worker first, then counts
active authoritative attempts in a second statement/snapshot. Full integration
and rebuilt-scheduler E2E passed after the fix.

## Remaining owner decision

- Select a repository license. No license was inferred or added.
