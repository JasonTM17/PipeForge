# Phase 5 review: dataset metadata and streamed uploads

## Scope

- Range: `ebf213c..7a5e42a`
- Focus: PostgreSQL dataset/version state, MinIO adapter, upload validation, HTTP ownership boundaries, Compose wiring, and public contract.
- Scout findings: pagination total on an empty page, missing object-store deadline/readiness check, production MinIO endpoint fallback, and incomplete state-history actor propagation.

## Spec compliance

| Requirement | Status | Evidence |
|---|---|---|
| Dataset/version/state-history schema with owner and lookup indexes | PASS | `go/migrations/000003_datasets.sql`; migration 3 applied in local PostgreSQL |
| MinIO Put/Head/Get/Delete adapter | PASS | `go/internal/storage/minio.go`; tagged lifecycle integration test |
| CSV/JSONL/Parquet, MIME/magic, filename, size, checksum validation | PASS | `go/internal/upload/validation.go`; validator tests and service tests |
| Stream upload without full-body buffering | PASS | bounded `io.LimitReader`; large 1 MiB generated-stream test reads at most `max+1` |
| Immutable replacement versions | PASS | version allocation/state transactions; real API smoke produced versions 1 and 2 |
| Ownership/scopes/pagination/filter/list/get/delete | PASS | service authorization tests, HTTP lifecycle tests, real cross-owner `403` and page-2 total check |
| Partial-failure cleanup | PASS | object deletion plus staged-version abort on size/checksum/storage failures |
| Public contract and local runtime documentation | PASS | OpenAPI YAML parse, docs-link validator, Compose config validation |

## Quality and adversarial review

### Accepted findings fixed

1. Repository pagination used `COUNT(*) OVER()` in the page query, so an empty page incorrectly returned `total=0`. Fixed with an independent count query and verified against the live API.
2. MinIO calls had no adapter-level deadline, and readiness checked PostgreSQL only. Added bounded operation contexts and bucket ping to readiness.
3. Non-development configuration defaulted MinIO endpoint to localhost. Production-like environments now require `MINIO_ENDPOINT` explicitly.
4. Upload state history recorded the dataset owner rather than the acting principal, and completion/abort omitted the actor. Actor identity now propagates through reserve/finalize/abort.

### Rejected concerns

- Object-key exposure: rejected as a finding because `DatasetVersion.ObjectKey` is explicitly JSON-omitted and HTTP tests assert it remains absent; no raw object route is registered.
- Filename path traversal: rejected as a finding because filename normalization rejects separators, dot paths, controls, and the filename is never used to construct the server-generated key.
- SQL injection in filters: rejected as a finding because all user values are bound parameters; `ILIKE` wildcard behavior is a documented search semantics issue, not SQL execution.

## Verification

- `go test ./...`: pass
- `go vet ./...`: pass
- `go test -tags=integration ./internal/storage`: pass against Compose MinIO
- `docker compose --env-file .env.example config --quiet`: pass
- Migration 3 applied; API live/ready returned `200`
- Real API smoke: registration `201`, dataset create `201`, two uploads `201`, detail showed two versions, cross-owner `403`, unsafe filename `400`, delete `204`
- MinIO object count matched committed version rows for the smoke fixtures

## Remaining scope

Multipart upload lifecycle, queue contracts, jobs, workers, processing, leases, quotas, CLI, observability expansion, CI, and release delivery remain in later CK phases. No Phase 5 blocker remains.

## Unresolved questions

- None blocking Phase 5. Multipart presign/checksum semantics are intentionally owned by Phase 6.
