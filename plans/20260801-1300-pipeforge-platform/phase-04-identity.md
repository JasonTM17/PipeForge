---
phase: 4
title: Identity
status: in-progress
priority: P1
effort: 4d
dependencies:
  - 3
---

# Phase 4: Identity

## Overview

Implement users, password authentication, refresh-token rotation, logout, hashed scoped API keys, role/scope authorization, ownership policies, and audit events without exposing secrets.

## Requirements

- Roles: `ADMIN`, `USER`, `SERVICE`; scopes are explicit and deny by default.
- Passwords and API keys are never stored or logged in plaintext.
- Refresh tokens rotate and replay is rejected; logout revokes the active token family/session.
- Authentication and authorization are tested at HTTP and policy boundaries.

## Architecture

The auth package owns credential hashing/token claims. Repositories own persistence. Middleware authenticates bearer/API-key credentials; policy functions authorize user/role/scope/resource ownership. Identity tables and audit events remain in PostgreSQL. Use Argon2id for passwords and a cryptographically random opaque refresh token whose hash is stored.

## Related Code Files

- Create: `go/internal/auth/`, `go/internal/authz/`, `go/internal/audit/`
- Create: `go/migrations/000002_identity.sql`
- Create/modify: `go/internal/httpapi/auth_handlers.go`, auth middleware, OpenAPI auth paths
- Create: `go/tests/auth_*_test.go`, `go/tests/authz_*_test.go`
- Modify: `README.md`, `.env.example`, docs security/API notes

## Implementation Steps

1. Add user, refresh-session/token-family, API-key, role/scope, and audit schemas with indexes and revocation timestamps.
2. Implement password hashing/verification, token issuance/rotation, secure comparison, and claim validation.
3. Add register/login/refresh/logout handlers with validation, status codes, rate-limit hooks, and correlation IDs.
4. Add API-key creation/list/revoke flow; return plaintext only at creation response and store only a hash/prefix.
5. Implement reusable policy checks for role, scope, ownership, and service credentials.
6. Add identity integration tests including refresh replay, wrong password, revoked key, missing scope, and cross-owner access.

## Success Criteria

- [ ] Registration/login/refresh/logout paths work with problem-style errors.
- [ ] Refresh-token rotation detects reuse and revokes the affected family.
- [ ] API-key secret is not persisted or returned after creation.
- [ ] Unauthenticated, insufficient-scope, and cross-owner requests are denied consistently.
- [ ] Sensitive values are absent from structured logs and test fixtures.

## Validation

- `go test ./...`
- `go vet ./...`
- targeted HTTP integration tests against Postgres
- secret-pattern scan over staged diff

## Risk Assessment

- Risk: token claims become an authorization source of truth. Mitigation: claims identify the principal; current role/scope/resource policy remains server-side and revocation-aware.
- Risk: API-key hash lookup is slow or ambiguous. Mitigation: unique key ID/prefix index and constant-time hash comparison.

## Security Considerations

Use Argon2id parameters documented in config, short-lived access tokens, refresh rotation, TLS-ready cookie/header rules, rate limiting hooks, audit logging, and no user enumeration in login errors.

## Next Steps

Phase 5 adds authorized dataset metadata and streamed object storage upload.

## Unresolved Questions

- None blocking; token TTL defaults will be conservative and configurable through validated environment settings.

