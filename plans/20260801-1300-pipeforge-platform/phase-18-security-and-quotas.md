---
phase: 18
title: "Security and quotas"
status: pending
priority: P1
effort: "4d"
dependencies: [4, 5, 8, 14, 17]
---

# Phase 18: Security and quotas

## Overview

Close trust-boundary gaps with hardened uploads/object keys, request/job/storage quotas, rate limiting, audit events, threat-model documentation, and adversarial security tests.

## Requirements

- Threat model covers malicious files, traversal, formula injection, object-key manipulation, SQL/message attacks, unauthorized artifacts, leakage, resource abuse, supply chain, and cross-owner exposure.
- Enforce maximum upload/body size, active/queued job and storage quotas, queue/worker limits, and request timeouts.
- Redact secrets/PII and sanitize generated CSV outputs against formula injection.
- Security controls are enforced at service boundaries, not only in clients.

## Architecture

Security policy helpers are shared by handlers/services, with PostgreSQL counters/quotas and bounded token-bucket/in-memory rate limit for single-node local deployment. Audit events are append-only and outbox-published where external consumers need them. The threat model maps asset → trust boundary → threat → control → residual risk.

## Related Code Files

- Create: `docs/security/threat-model.md`, `docs/security/secure-configuration.md`
- Create: `go/internal/security/`, `go/internal/quotas/`, `go/migrations/000011_security_quotas_audit.sql`
- Modify: upload, authz, job, artifact, queue, logging, report serialization
- Create: malicious input, authorization, quota/rate-limit, secret-redaction tests
- Modify: `SECURITY.md`, `.env.example`, Docker hardening files

## Implementation Steps

1. Write threat model and map controls to concrete code/test locations.
2. Harden filename/content/compression/object-key/checksum/presigned URL handling and CSV output.
3. Add request/user quotas, rate limits, active-job/queued-volume admission, and meaningful 429/413/409 errors.
4. Add audit events for auth, admin, replay, ownership denial, and security-relevant mutations.
5. Add dependency/container/secret scanning configuration and security regression tests.

## Success Criteria

- [ ] Threat model has no unassigned high-risk threat in MVP scope.
- [ ] Oversized, malicious, traversal, formula, injection, and cross-owner cases are rejected or sanitized.
- [ ] Quotas/rate limits are enforced consistently and produce observable audit/metrics events.
- [ ] No sensitive data appears in logs, reports, errors, fixtures, or Docker layers.
- [ ] Security tests pass without weakening existing behavior.

## Validation

- Go/Python security test suites
- static secret/dependency scan
- container filesystem/user inspection
- API authorization matrix and quota load smoke test

## Risk Assessment

- Risk: local in-memory rate limiting is not distributed. Mitigation: document single-node scope and leave a replaceable limiter interface; do not claim multi-replica enforcement.
- Risk: security hardening breaks valid formats. Mitigation: regression fixtures and explicit error codes for every validation rejection.

## Security Considerations

This phase is the primary security gate: least privilege, secure defaults, bounded resource use, output redaction, auditability, dependency pinning/scanning, and non-root containers are mandatory.

## Next Steps

Phase 19 adds cross-service observability and operational dashboards/runbooks.

## Unresolved Questions

- None blocking; production secret-manager integration is documented as deployment guidance, not implemented locally.

