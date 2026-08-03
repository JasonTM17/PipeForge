---
phase: 1
title: API security and resilience
status: completed
priority: P1
effort: 6h
dependencies: []
---

# Phase 1: API security and resilience

## Overview

Bound public API resource use and credential-endpoint abuse without changing
HTTP routes, successful payloads, identity storage, or ownership semantics.

## Requirements

- Configure read-header, read, write, idle, and maximum-header server bounds.
- Add an in-memory per-client-IP limiter to register/login/refresh endpoints.
- Do not trust forwarded headers without an explicit trusted-proxy policy.
- Return the existing problem envelope with HTTP 429 and `Retry-After`.
- Bound `pipectl` HTTP calls so CLI operations cannot hang indefinitely.

## Related code files

- Modify: `go/internal/platform/config/config.go`, tests, `.env.example`.
- Modify: `go/cmd/api/main.go`, `go/cmd/pipectl/main.go` and tests.
- Create: `go/internal/httpapi/auth-rate-limit.go` and tests.
- Modify: `go/internal/httpapi/router.go`, auth route registration, OpenAPI/docs.

## Tests before

- Add failing tests for burst rejection, IP isolation, cleanup/expiry, and 429 body.
- Add config tests for invalid/valid timeout and limiter settings.

## Implementation Steps

1. Extend validated config with conservative development defaults.
2. Implement concurrency-safe, bounded-entry auth limiter with injectable clock.
3. Wrap only public credential routes; preserve authenticated API-key routes.
4. Configure `http.Server` bounds and CLI client timeout.
5. Update API/security configuration documentation.
6. Run focused tests, race detector, vet, and full Go suite.

## Success Criteria

- [ ] Auth burst returns 429 problem response and `Retry-After`.
- [ ] Distinct remote addresses do not share allowance.
- [ ] Limiter state expires and cannot grow without bound.
- [ ] API and CLI timeouts are validated and tested.
- [ ] Go test/race/vet/build pass with unchanged public success contracts.

## Risk assessment

False positives could block legitimate local automation. Use configurable defaults,
scope the limiter to credential routes, and test burst/refill semantics. In-memory
state is intentionally process-local and documented; distributed limiting remains
a deployment concern.
