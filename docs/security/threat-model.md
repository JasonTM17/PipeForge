# Threat model

This model covers the verified local PipeForge architecture. It maps current
controls to concrete implementation boundaries and states residual risks
without claiming production certification.

## Assets and trust boundaries

| Asset | Boundary | Primary controls |
|---|---|---|
| Passwords, refresh tokens, API keys | Internet/client → Go API → PostgreSQL | Argon2id passwords; opaque 256-bit tokens stored as SHA-256 lookup digests; rotation/reuse revocation; scoped API keys |
| Dataset bytes and artifacts | Client → Go API → MinIO → Python worker | Owner authorization; upload and multipart limits; server-owned object keys; immutable versions and attempt-scoped artifacts; canonical promotion after fenced result acceptance |
| Job state and quotas | API/scheduler/result consumer → PostgreSQL | PostgreSQL source of truth; transactions; row locks; owner-scoped queued quota; lease fencing; bounded retry and DLQ |
| Commands and results | Go services ↔ RabbitMQ ↔ workers | Versioned JSON Schemas; durable exchanges/queues; per-worker UUID routes; inbox/outbox idempotency; attempt and lease validation |
| Browser session | Browser → Go API | Session-storage access token; no refresh token or storage credentials in the bundle; 401 cleanup; superseded-request cancellation |
| Build and release identity | GitHub Actions → GHCR/Docker Hub | Protected branch checks; pinned release actions; least-privilege job permissions; SBOM, provenance, attestation, digest evidence and Trivy gate |

## Threats, controls, and residual risk

| Threat | Implemented control | Residual risk / required production work |
|---|---|---|
| Credential stuffing and auth resource abuse | Bounded per-peer token bucket on register/login/refresh; generic auth errors; body limits and HTTP timeouts | Limiter is process-local and intentionally ignores forwarded headers. Multi-replica or proxied deployment needs a shared limiter and explicit trusted-proxy chain. |
| Password or token database disclosure | Argon2id password hashing; high-entropy opaque tokens/API keys stored only as lookup digests; short-lived access JWTs | Rotate signing keys and credentials through a secret manager; define compromise and mass-revocation procedures. |
| Cross-owner dataset/job/artifact access | Current role/scopes loaded from PostgreSQL; ownership checks at service boundaries; raw object keys omitted | Run an external authorization review before hosting untrusted tenants. Admin bypass remains intentionally powerful. |
| Path traversal or attacker-controlled object keys | Object keys are generated from validated UUID/domain identifiers; clients do not supply filesystem paths or canonical object keys | MinIO bucket policy and network isolation are deployment responsibilities. |
| Oversized or slow uploads | Content-length and streamed byte limits; bounded multipart part/count/total limits; server read/write/header limits | No tenant storage accounting or distributed bandwidth limiter; reverse proxy limits are still required. |
| Malformed CSV/JSONL/Parquet or algorithmic resource abuse | Format-specific readers, schema validation, bounded chunks, worker concurrency/prefetch, job quotas and operation allow-list | Crafted parser inputs still need ongoing fuzzing; worker CPU/memory limits are not enforced by local Compose. |
| SQL, message, or config injection | Parameterized SQL; typed operation validation; versioned JSON Schemas; no arbitrary Python expression execution | New operations and schema versions require boundary review and negative fixtures. |
| Duplicate, stale, or forged results | Transactional outbox/inbox, message IDs, attempt fencing, lease ownership, immutable attempt objects and canonical promotion | Late duplicates may leave unreferenced objects until an object reaper exists. |
| Queue hijacking or wrong-worker cancellation | UUID-suffixed job/cancellation queues and exact routing keys; legacy shared consumers disabled by default; live two-worker E2E proof | Worker UUID uniqueness is an operator invariant; broker TLS and credentials are not configured by local defaults. |
| Browser stale-response/session confusion | AbortController on refresh/artifact requests, session generation fencing, 401 session reset, selected-job validity check and URL synchronization | Access tokens remain readable by same-origin JavaScript; production browser auth should use a reviewed cookie/BFF design and CSP. |
| Secret or PII leakage | Stable problem responses; credential redaction rules; tests and E2E output omit tokens, presigned URLs and rows | Application logs require centralized access controls and retention policy in a real deployment. |
| Dependency or image compromise | Dependabot, dependency review, CodeQL, npm audit, non-root images, Trivy HIGH/CRITICAL gate, SBOM/provenance/attestation | External base images and registries remain supply-chain dependencies; establish update SLAs and signature policy. |
| Data loss or regional outage | PostgreSQL is authoritative and state transitions are transactional | Local Compose has no HA, tested backup/restore, disaster recovery, multi-region design or SLO. |

## Security invariants

- Python workers never mutate authoritative job-state tables.
- A result becomes canonical only for the active fenced attempt.
- Public APIs never expose MinIO credentials or internal object keys.
- Retry, queue admission, cleanup, pagination, request sizes, and concurrency are
  bounded.
- No production claim is valid until the residual deployment risks above have
  owners, tests, and operational evidence.

## Verification

Relevant automated evidence includes Go authorization, identity, quota, lease,
result and rate-limit tests; Python reader/processing tests; contract negative
fixtures; desktop/mobile console tests; CodeQL/dependency gates; container
scanning; and the real multi-worker Compose flow. See
[validation evidence](../release/validation-evidence.md) and
[secure configuration](./secure-configuration.md).
