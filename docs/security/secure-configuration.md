# Secure configuration

The checked-in `.env.example` is for isolated local evaluation only. Do not
reuse any example value in a shared environment.

## Before a shared deployment

1. Generate independent high-entropy PostgreSQL, RabbitMQ, MinIO, Grafana, and
   JWT credentials; inject them from a secret manager.
2. Terminate TLS at a reviewed proxy, restrict direct service ports, and define
   exactly which proxy addresses may supply client-IP headers. PipeForge does
   not trust forwarded headers by default.
3. Configure API header/read/write/idle timeouts and upload/multipart limits for
   the largest approved workload. Keep every value bounded.
4. Tune auth burst/rate/client-cap/TTL settings. Use a shared limiter before
   running multiple API replicas.
5. Assign a unique `PIPEFORGE_WORKER_ID` to every concurrent worker. Keep
   legacy shared-queue compatibility disabled except for one controlled
   upgrade drainer.
6. Apply least-privilege network and bucket policies; workers need dataset read
   and attempt-artifact write access, never direct job-table mutation.
7. Pin published images by immutable digest and verify GitHub attestation.
8. Define backup, restore, key rotation, audit retention, incident response,
   vulnerability remediation, capacity, and SLO procedures before real data.

## Browser deployment

The current console is a learning surface using session storage. A public
deployment must add a reviewed CSP, TLS-only origin, clickjacking protection,
same-origin API policy, and a browser-auth design appropriate to its threat
model. Never place refresh tokens, MinIO credentials, presigned URLs, or private
configuration in the frontend bundle.

## Validation commands

```text
docker compose config --quiet
python scripts/validate-contracts.py
python scripts/validate-docs.py
go test -race ./...
npm run test:coverage
npm run test:browser
```

Run Go commands from `go/` and npm commands from `frontend/`. Release images
must additionally pass the publication workflow's vulnerability, non-root,
entrypoint, provenance and digest checks.
